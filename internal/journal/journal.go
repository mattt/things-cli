// Package journal persists mutation intent before URL dispatch.
package journal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const retention = 90 * 24 * time.Hour

type Journal struct {
	db  *sql.DB
	dir string
}

func defaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "things-cli"), nil
}

func Open(dir string) (*Journal, error) {
	if dir == "" {
		var err error
		dir, err = defaultDir()
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, domain.E("journal_access", "Cannot create the private operation directory.", 4)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "operations.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, domain.E("journal_access", "Cannot open the operation journal.", 4)
	}
	f.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}

	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": {"busy_timeout(1500)", "synchronous(FULL)"}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, request_key TEXT UNIQUE, request_hash TEXT NOT NULL, body BLOB NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS dispatches (at INTEGER NOT NULL, items INTEGER NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, domain.E("journal_access", "Cannot initialize the operation journal.", 4)
	}

	return &Journal{db, dir}, nil
}

func (j *Journal) Close() error { return j.db.Close() }

func (j *Journal) Lock(ctx context.Context) (func(), error) {
	f, err := os.OpenFile(filepath.Join(j.dir, "write.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, domain.E("journal_access", "Cannot lock the operation journal.", 4)
	}

	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				unix.Flock(int(f.Fd()), unix.LOCK_UN)
				f.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			f.Close()
			return nil, err
		}

		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (j *Journal) Get(ctx context.Context, id string) (domain.Operation, error) {
	return j.get(ctx, "id", id)
}

func (j *Journal) ByKey(ctx context.Context, key string) (domain.Operation, error) {
	return j.get(ctx, "request_key", key)
}

func (j *Journal) get(ctx context.Context, column, value string) (domain.Operation, error) {
	var b []byte
	err := j.db.QueryRowContext(ctx, "SELECT body FROM operations WHERE "+column+" = ?", value).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Operation{}, domain.E("operation_missing", "No matching operation was found.", 3)
	}
	if err != nil {
		return domain.Operation{}, err
	}

	var o domain.Operation
	err = json.Unmarshal(b, &o)
	if err != nil {
		return o, domain.E("journal_corrupt", "Cannot decode an operation journal record.", 4)
	}
	return o, nil
}

func (j *Journal) Save(ctx context.Context, o domain.Operation) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}

	var key any
	if o.RequestKey != "" {
		key = o.RequestKey
	}

	_, err = j.db.ExecContext(ctx,
		`INSERT INTO operations(id,request_key,request_hash,body,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,updated_at=excluded.updated_at`,
		o.ID, key, o.RequestHash, b, o.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.E("journal_write_failed", "Cannot persist the operation outcome. Inspect the journal before retrying.", 4)
	}
	return nil
}

// ReserveRate is called while holding the process lock, before dispatch.
func (j *Journal) ReserveRate(ctx context.Context, count int) error {
	now := time.Now().UnixMilli()
	var used int
	if _, err := j.db.ExecContext(ctx, "DELETE FROM dispatches WHERE at < ?", now-10000); err != nil {
		return err
	}
	if err := j.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(items),0) FROM dispatches WHERE at >= ?", now-10000).Scan(&used); err != nil {
		return err
	}

	if used+count > 250 {
		return domain.E("write_rate_limit", "Things permits 250 new items per 10 seconds. Wait before starting another write.", 6)
	}

	_, err := j.db.ExecContext(ctx, "INSERT INTO dispatches(at,items) VALUES(?,?)", now, count)
	return err
}

func (j *Journal) Prune(ctx context.Context, now time.Time) (int, error) {
	unlock, err := j.Lock(ctx)
	if err != nil {
		return 0, err
	}
	defer unlock()

	rows, err := j.db.QueryContext(ctx, "SELECT body FROM operations")
	if err != nil {
		return 0, err
	}
	ids := []string{}
	needed := map[string]bool{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return 0, err
		}

		var o domain.Operation
		if err := json.Unmarshal(b, &o); err != nil {
			rows.Close()
			return 0, err
		}
		if o.Outcome == "confirmed" || o.Outcome == "failed" {
			if o.UpdatedAt.Before(now.Add(-retention)) {
				ids = append(ids, o.ID)
			}
		} else {
			for _, child := range o.Children {
				needed[child.ID] = true
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, id := range ids {
		if needed[id] {
			continue
		}
		if _, err := j.db.ExecContext(ctx, "DELETE FROM operations WHERE id = ?", id); err != nil {
			return 0, err
		}
		removed++
	}
	return removed, nil
}

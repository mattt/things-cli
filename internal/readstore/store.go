package readstore

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"golang.org/x/sys/unix"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/mattt/things-cli/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct {
	db       *sql.DB
	version  int
	database string
}

func Open(ctx context.Context, path string) (*Store, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, domain.E("database_access", "Cannot read the Things database. Check its path and macOS privacy permissions.", 4)
	}
	info, err := f.Stat()
	f.Close()
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return nil, err
	}
	identity := domain.Hash([]any{canonical, stat.Dev, stat.Ino, info.Mode().Type()})

	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(1500)", "query_only(1)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, domain.E("database_access", "Cannot open the Things database.", 4)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	s := &Store{db: db, database: identity}
	if err := s.probe(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) probe(ctx context.Context) error {
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM Meta WHERE key = ?", "databaseVersion").Scan(&raw); err != nil {
		return domain.E("schema_unsupported", "Cannot read the Things schema version.", 5)
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		var p struct {
			Integer int `xml:"integer"`
		}
		err = xml.Unmarshal([]byte(raw), &p)
		v = p.Integer
	}
	if err != nil || v < 22 || v > 29 {
		return domain.E("schema_unsupported", fmt.Sprintf("Things database version %d is unsupported; this adapter supports versions 22 through 29.", v), 5)
	}

	required := map[string][]string{
		"TMTask": {
			"uuid",
			"type",
			"status",
			"title",
			"notes",
			"trashed",
			"start",
			"startDate",
			"startBucket",
			"deadline",
			"deadlineSuppressionDate",
			"reminderTime",
			"index",
			"todayIndex",
			"area",
			"project",
			"heading",
			"creationDate",
			"userModificationDate",
			"stopDate",
			"rt1_recurrenceRule",
			"rt1_repeatingTemplate",
		},
		"TMArea":          {"uuid", "title", "index"},
		"TMTag":           {"uuid", "title", "index"},
		"TMTaskTag":       {"tasks", "tags"},
		"TMAreaTag":       {"areas", "tags"},
		"TMChecklistItem": {"uuid", "title", "status", "task", "index"},
	}
	for table, cols := range required {
		rows, err := s.db.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
		if err != nil {
			return domain.E("schema_unsupported", "Cannot inspect the Things schema.", 5)
		}

		found := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			found[name] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}

		for _, col := range cols {
			if !found[col] {
				return domain.E("schema_unsupported", "Missing required Things field: "+table+"."+col, 5)
			}
		}
	}

	s.version = v
	return nil
}

func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, readError(err)
	}
	defer tx.Rollback()

	snap := Snapshot{Items: []domain.Item{}, ObservedAt: time.Now().UTC(), Database: s.database, Version: s.version}
	rows, err := tx.QueryContext(ctx,
		`SELECT uuid,type,status,COALESCE(title,''),COALESCE(notes,''),trashed,start,startDate,startBucket,deadline,deadlineSuppressionDate,reminderTime,"index",todayIndex,COALESCE(area,''),COALESCE(project,''),COALESCE(heading,''),creationDate,userModificationDate,stopDate,rt1_recurrenceRule,COALESCE(rt1_repeatingTemplate,'') FROM TMTask`,
	)
	if err != nil {
		return Snapshot{}, readError(err)
	}
	for rows.Next() {
		i := emptyItem()
		var kind, status, start, bucket int
		var trash bool
		var sd, dd, rem, supp sql.NullInt64
		var created, modified, stopped sql.NullFloat64
		var recurrence any
		if err := rows.Scan(
			&i.ID, &kind, &status, &i.Title, &i.Notes, &trash,
			&start, &sd, &bucket, &dd, &supp, &rem,
			&i.Index, &i.TodayIndex, &i.AreaID, &i.ProjectID, &i.HeadingID,
			&created, &modified, &stopped, &recurrence, &i.TemplateID,
		); err != nil {
			rows.Close()
			return Snapshot{}, readError(err)
		}

		kinds := map[int]string{0: "todo", 1: "project", 2: "heading"}
		starts := map[int]string{0: "inbox", 1: "anytime", 2: "someday"}
		var ok bool
		i.Kind, ok = kinds[kind]
		if !ok {
			rows.Close()
			return Snapshot{}, domain.E("schema_decode", "Unknown Things item kind.", 5)
		}
		i.Status, err = decodeStatus(status)
		if err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		i.Start, ok = starts[start]
		if !ok {
			rows.Close()
			return Snapshot{}, domain.E("schema_decode", "Unknown Things scheduling state.", 5)
		}

		i.Trashed = trash
		if bucket != 0 && bucket != 1 {
			rows.Close()
			return Snapshot{}, domain.E("schema_decode", "Unknown Things day or evening bucket.", 5)
		}
		i.Evening = bucket == 1
		i.DeadlineSuppressed = supp.Valid

		i.StartDate, err = decodeDate(sd)
		if err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		i.Deadline, err = decodeDate(dd)
		if err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		i.Reminder, err = decodeTime(rem)
		if err != nil {
			rows.Close()
			return Snapshot{}, err
		}

		i.CreatedAt = timestamp(created)
		i.ModifiedAt = timestamp(modified)
		i.StoppedAt = timestamp(stopped)

		if recurrence != nil {
			i.Recurrence = "template"
		} else if i.TemplateID != "" {
			i.Recurrence = "occurrence"
		} else {
			i.Recurrence = "none"
		}
		i.Capabilities["recurrence_rule"] = "unavailable: opaque Things format"

		snap.Items = append(snap.Items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Snapshot{}, readError(err)
	}

	for _, kind := range []string{"area", "tag"} {
		table := "TMArea"
		if kind == "tag" {
			table = "TMTag"
		}

		rows, err := tx.QueryContext(ctx, `SELECT uuid,COALESCE(title,''),"index" FROM `+table)
		if err != nil {
			return Snapshot{}, readError(err)
		}
		for rows.Next() {
			i := emptyItem()
			i.Kind = kind
			i.Status = "unavailable"
			i.Start = "unavailable"
			if err := rows.Scan(&i.ID, &i.Title, &i.Index); err != nil {
				rows.Close()
				return Snapshot{}, readError(err)
			}
			for _, f := range []string{"notes", "status", "scheduling", "timestamps", "recurrence"} {
				i.Capabilities[f] = "unavailable for " + kind
			}
			snap.Items = append(snap.Items, i)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Snapshot{}, readError(err)
		}
	}

	byID := map[string]*domain.Item{}
	for n := range snap.Items {
		i := &snap.Items[n]
		if i.ID == "" || byID[i.ID] != nil {
			return Snapshot{}, domain.E("schema_decode", "Empty or duplicate Things item ID.", 5)
		}
		byID[i.ID] = i
	}

	for _, rel := range []struct {
		table, left string
	}{
		{"TMTaskTag", "tasks"},
		{"TMAreaTag", "areas"},
	} {
		rows, err := tx.QueryContext(ctx, `SELECT `+rel.left+`,tags FROM `+rel.table)
		if err != nil {
			return Snapshot{}, readError(err)
		}
		for rows.Next() {
			var item, tag string
			if err := rows.Scan(&item, &tag); err != nil {
				rows.Close()
				return Snapshot{}, readError(err)
			}
			if i, t := byID[item], byID[tag]; i != nil && t != nil {
				i.Tags = append(i.Tags, domain.Tag{ID: t.ID, Title: t.Title})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Snapshot{}, readError(err)
		}
	}

	rows, err = tx.QueryContext(ctx, `SELECT uuid,COALESCE(title,''),status,task FROM TMChecklistItem ORDER BY "index",uuid`)
	if err != nil {
		return Snapshot{}, readError(err)
	}
	for rows.Next() {
		var c domain.ChecklistItem
		var status int
		var task string
		if err := rows.Scan(&c.ID, &c.Title, &status, &task); err != nil {
			rows.Close()
			return Snapshot{}, readError(err)
		}
		c.Status, err = decodeStatus(status)
		if err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		if i := byID[task]; i != nil {
			i.Checklist = append(i.Checklist, c)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Snapshot{}, readError(err)
	}

	for n := range snap.Items {
		i := &snap.Items[n]
		if !utf8.ValidString(i.ID) || !utf8.ValidString(i.Title) || !utf8.ValidString(i.Notes) {
			return Snapshot{}, domain.E("schema_decode", "Things contains invalid UTF-8 item text.", 5)
		}
		sort.Slice(i.Tags, func(a, b int) bool { return i.Tags[a].ID < i.Tags[b].ID })

		if h := byID[i.HeadingID]; h != nil {
			if i.ProjectID == "" {
				i.ProjectID = h.ProjectID
			}
			i.ContextTrashed = h.Trashed
		}
		if p := byID[i.ProjectID]; p != nil {
			if i.AreaID == "" {
				i.AreaID = p.AreaID
			}
			i.ContextTrashed = i.ContextTrashed || p.Trashed
		}

		i.Fingerprint = domain.Fingerprint(*i)
	}

	if err := tx.Commit(); err != nil {
		return Snapshot{}, readError(err)
	}
	return snap, nil
}

func emptyItem() domain.Item {
	return domain.Item{
		Tags:         []domain.Tag{},
		Checklist:    []domain.ChecklistItem{},
		Capabilities: map[string]string{},
	}
}

func readError(err error) error {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return err
	}
	return domain.E("database_read_failed", "Cannot read the Things database. Check access, schema compatibility, and whether the database is busy.", 4)
}

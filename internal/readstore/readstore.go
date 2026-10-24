// Package readstore adapts the Things v22+ schema to domain records.
// Date decoding and schema relationships are derived from things.py.
package readstore

import (
	"context"
	"os"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

type Snapshot struct {
	Items      []domain.Item
	ObservedAt time.Time
	Database   string
	Version    int
}

type Reader interface {
	Snapshot(context.Context) (Snapshot, error)
}

type Lazy struct{ Path string }

func (l Lazy) Snapshot(ctx context.Context) (Snapshot, error) {
	path, err := Discover(l.Path)
	if err != nil {
		return Snapshot{}, err
	}
	s, err := Open(ctx, path)
	if err != nil {
		return Snapshot{}, err
	}
	defer s.Close()
	return s.Snapshot(ctx)
}

// CheckWriteDatabase permits URL dispatch only for the single discovered library.
func (l Lazy) CheckWriteDatabase() error {
	selected, err := Discover(l.Path)
	if err != nil {
		return err
	}
	active, err := Discover("")
	if err != nil {
		return err
	}
	a, err := os.Stat(active)
	if err != nil {
		return err
	}
	b, err := os.Stat(selected)
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return domain.E("write_database_mismatch", "Writes require the single discovered Things library. Other databases support reads and dry runs only.", 5)
	}
	return nil
}

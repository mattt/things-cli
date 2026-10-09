// Package readstore adapts the Things v22+ schema to domain records.
// Date decoding and schema relationships are derived from things.py.
package readstore

import (
	"context"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

type Snapshot struct {
	Items      []domain.Item
	ObservedAt time.Time
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

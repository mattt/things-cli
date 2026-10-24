// Package service implements the common CLI and MCP contract.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/journal"
	"github.com/mattt/things-cli/internal/keychain"
	"github.com/mattt/things-cli/internal/readstore"
	"github.com/mattt/things-cli/internal/urlscheme"
)

type Service struct {
	Reader        readstore.Reader
	Journal       *journal.Journal
	Tokens        keychain.Store
	Dispatcher    urlscheme.Dispatcher
	VerifyTimeout time.Duration
	PollInterval  time.Duration
	Now           func() time.Time
	Callbacks     bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func str(v string) *string { return &v }

func requestHash(r domain.Request) string {
	r.OperationKey = ""
	r.DryRun = false
	return domain.Hash(r)
}

// sortByIndex orders items as Things displays them, breaking ties by ID.
func sortByIndex(items []domain.Item) {
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Index != items[b].Index {
			return items[a].Index < items[b].Index
		}
		return items[a].ID < items[b].ID
	})
}

func (s *Service) Operation(ctx context.Context, id string, reconcile bool) (domain.Operation, error) {
	if s.Journal == nil {
		return domain.Operation{}, domain.E("journal_required", "The operation journal is unavailable.", 4)
	}
	if !reconcile {
		return s.Journal.Get(ctx, id)
	}

	unlock, err := s.Journal.Lock(ctx)
	if err != nil {
		return domain.Operation{}, err
	}
	defer unlock()

	o, err := s.Journal.Get(ctx, id)
	if err != nil {
		return o, err
	}
	return s.reconcile(ctx, o)
}

func (s *Service) reconcile(ctx context.Context, o domain.Operation) (domain.Operation, error) {
	if o.Outcome == "confirmed" || o.Outcome == "failed" {
		return o, nil
	}

	if o.Request.Action == "batch" {
		for n, c := range o.Children {
			latest, err := s.Journal.Get(ctx, c.ID)
			if err == nil {
				c = latest
			}
			if c.Outcome == "unknown" {
				c, err = s.reconcile(ctx, c)
				if err != nil {
					return o, err
				}
			}
			o.Children[n] = c
		}
		aggregate(&o)
		return s.persist(ctx, o)
	}
	if o.State == "prepared" {
		return s.failPrepared(ctx, o, domain.E("not_dispatched", "The operation stopped before dispatch. No write was sent.", 6))
	}

	snap, err := s.Reader.Snapshot(ctx)
	if err != nil {
		return o, err
	}
	if err := checkOperationDatabase(o, snap); err != nil {
		return o, err
	}
	if !s.observe(&o, snap) {
		o.State = "unknown"
		o.Outcome = "unknown"
	}
	return s.persist(ctx, o)
}

func (s *Service) checkWriteDatabase() error {
	if reader, ok := s.Reader.(interface{ CheckWriteDatabase() error }); ok {
		return reader.CheckWriteDatabase()
	}
	return nil
}

func checkOperationDatabase(o domain.Operation, snap readstore.Snapshot) error {
	if o.Database != snap.Database {
		return domain.E("operation_database_mismatch", "Select the database recorded for this operation. Records without a database identity cannot be reconciled against a Things database.", 5)
	}
	return nil
}

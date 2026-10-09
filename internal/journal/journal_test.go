package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

func TestJournalPrivacyAndPruning(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	for _, test := range []struct {
		path string
		mode os.FileMode
	}{
		{dir, 0700},
		{filepath.Join(dir, "operations.sqlite"), 0600},
	} {
		info, err := os.Stat(test.path)
		if err != nil || info.Mode().Perm() != test.mode {
			t.Fatal(test, info, err)
		}
	}

	now := time.Now().UTC()
	for _, o := range []domain.Operation{
		{
			ID:          "old",
			RequestKey:  "old-key",
			RequestHash: "hash",
			State:       "verified",
			Outcome:     "confirmed",
			UpdatedAt:   now.Add(-91 * 24 * time.Hour),
		},
		{
			ID:          "unknown",
			RequestKey:  "unknown-key",
			RequestHash: "hash",
			State:       "dispatching",
			Outcome:     "unknown",
			UpdatedAt:   now.Add(-91 * 24 * time.Hour),
		},
		{
			ID:          "recent",
			RequestHash: "hash",
			State:       "verified",
			Outcome:     "confirmed",
			UpdatedAt:   now,
		},
	} {
		if err := j.Save(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}

	count, err := j.Prune(context.Background(), now)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := j.ByKey(context.Background(), "old-key"); domain.AsError(err).Code != "operation_missing" {
		t.Fatal(err)
	}
	o, err := j.Get(context.Background(), "unknown")
	if err != nil || o.State != "dispatching" {
		t.Fatal(o, err)
	}

	failed := domain.Operation{
		ID:          "failed",
		RequestHash: "hash",
		Outcome:     "failed",
		State:       "failed",
		Errors:      []*domain.Error{domain.E("write_rejected", "Rejected.", 6)},
		UpdatedAt:   now,
	}
	if err := j.Save(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
	saved, err := j.Get(context.Background(), failed.ID)
	if err != nil || domain.AsError(saved.ResultError()).Exit != 6 {
		t.Fatal("stored failures lost their exit code", saved, err)
	}
}

func TestLockCancellationAndRateLimit(t *testing.T) {
	j, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	unlock, err := j.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := j.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lock ignored deadline", err)
	}

	if err := j.ReserveRate(context.Background(), 250); err != nil {
		t.Fatal(err)
	}
	if err := j.ReserveRate(context.Background(), 1); domain.AsError(err).Code != "write_rate_limit" {
		t.Fatal(err)
	}
}

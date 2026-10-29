package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

func TestCreateResponsesKeepBaselinePrivate(t *testing.T) {
	for _, action := range []string{"create_todo", "create_project"} {
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/batch=%t", action, batch), func(t *testing.T) {
				ctx := context.Background()
				items := make([]domain.Item, 10000)
				for n := range items {
					items[n] = todo(fmt.Sprintf("existing-%022d", n), "New")
				}
				s, m := testService(t, items...)
				request := domain.Request{Action: action, Patch: domain.Patch{Title: str("New")}}
				s.Dispatcher = dispatchFunc(func(context.Context, string) error { return nil })
				var o domain.Operation
				var err error
				if batch {
					o, err = s.Batch(ctx, domain.BatchRequest{Actions: []domain.Request{request}, OperationKey: "create"})
				} else {
					request.OperationKey = "create"
					o, err = s.Mutate(ctx, request)
				}
				if err != nil {
					t.Fatal(err)
				}
				if o.Outcome != "unknown" {
					t.Fatalf("existing items must not confirm creation: %s", o.Outcome)
				}
				check := func(o domain.Operation) {
					t.Helper()
					b, err := json.Marshal(domain.Wrap(o, "urlscheme", time.Time{}))
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(b), "baseline_ids") || strings.Contains(string(b), "existing-") {
						t.Fatalf("public response contains baseline IDs (%d bytes)", len(b))
					}
					if len(b) > 5000 {
						t.Fatalf("response too large: %d bytes", len(b))
					}
					t.Logf("public response: %d bytes", len(b))
				}
				check(o)
				saved, err := s.Journal.Get(ctx, o.ID)
				if err != nil {
					t.Fatal(err)
				}
				baseline := saved.BaselineIDs
				if batch {
					baseline = saved.Children[0].BaselineIDs
				}
				if len(baseline) != len(items) {
					t.Fatalf("journal baseline lost: %d", len(baseline))
				}
				// Reconciliation must still exclude the matching pre-existing items.
				old, err := s.Operation(ctx, o.ID, true)
				if err != nil || old.Outcome != "unknown" {
					t.Fatalf("unsafe reconciliation: %s, %v", old.Outcome, err)
				}
				check(old)
				m.mu.Lock()
				created := todo("created-item", "New")
				if action == "create_project" {
					created.Kind = "project"
				}
				m.items = append(m.items, created)
				m.mu.Unlock()
				confirmed, err := s.Operation(ctx, o.ID, true)
				if err != nil || confirmed.Outcome != "confirmed" {
					t.Fatalf("creation not verified: %s, %v", confirmed.Outcome, err)
				}
				if len(confirmed.ItemIDs) != 1 || confirmed.ItemIDs[0] != "created-item" {
					t.Fatalf("wrong created IDs: %v", confirmed.ItemIDs)
				}
				check(confirmed)
				var replay domain.Operation
				if batch {
					replay, err = s.Batch(ctx, domain.BatchRequest{Actions: []domain.Request{request}, OperationKey: "create"})
				} else {
					replay, err = s.Mutate(ctx, request)
				}
				if err != nil || replay.ID != o.ID {
					t.Fatalf("replay failed: %v", err)
				}
				check(replay)
			})
		}
	}
}

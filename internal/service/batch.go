package service

import (
	"context"

	"github.com/mattt/things-cli/internal/domain"
)

func (s *Service) Batch(ctx context.Context, b domain.BatchRequest) (domain.Operation, error) {
	if len(b.Actions) == 0 || len(b.Actions) > 250 {
		return domain.Operation{}, domain.E("batch_limit", "A batch must contain 1 through 250 actions.", 2)
	}
	if len(b.OperationKey) > 256 {
		return domain.Operation{}, domain.E("invalid_operation_key", "Operation keys must be at most 256 bytes.", 2)
	}

	unlock, err := s.Journal.Lock(ctx)
	if err != nil {
		return domain.Operation{}, err
	}
	defer unlock()

	normalized := b
	normalized.OperationKey = ""
	normalized.DryRun = false
	hash := domain.Hash(normalized)

	var existing *domain.Operation
	if b.OperationKey != "" && !b.DryRun {
		old, err := s.Journal.ByKey(ctx, b.OperationKey)
		if err == nil {
			if old.InputHash == hash {
				if old.Outcome == "unknown" || old.Outcome == "partial" {
					old, err = s.reconcile(ctx, old)
				}
				return old, err
			}
			existing = &old
		}
		if err != nil && domain.AsError(err).Code != "operation_missing" {
			return domain.Operation{}, err
		}
	}

	database := ""
	plans := []domain.Request{}
	count := 0
	updates := map[string]bool{}
	for _, input := range b.Actions {
		if input.OperationKey != "" || input.DryRun {
			return domain.Operation{}, domain.E("invalid_batch_action", "Set the operation key and dry-run flag on the parent batch.", 2)
		}

		r, snap, _, err := s.prepare(ctx, input)
		if err != nil {
			if existing != nil {
				return *existing, domain.E("operation_key_conflict", "This operation key belongs to a different batch.", 2)
			}
			return domain.Operation{}, err
		}

		if database != "" && database != snap.Database {
			return domain.Operation{}, domain.E("operation_database_mismatch", "The database changed during batch validation.", 5)
		}
		database = snap.Database
		if r.ID != "" {
			if updates[r.ID] {
				return domain.Operation{}, domain.E("duplicate_batch_target", "A batch can update each item only once. Combine its fields in one patch.", 2)
			}
			updates[r.ID] = true
		}
		if r.Action == "create_todo" || r.Action == "create_project" {
			count += 1 + len(r.Items)
		}
		plans = append(plans, r)
	}

	canonicalHash := domain.Hash(domain.BatchRequest{Actions: plans})
	if existing != nil {
		if existing.RequestHash != canonicalHash {
			return *existing, domain.E("operation_key_conflict", "This operation key belongs to a different batch.", 2)
		}
		old := *existing
		if old.Outcome == "unknown" || old.Outcome == "partial" {
			old, err = s.reconcile(ctx, old)
		}
		return old, err
	}

	if count > 250 {
		return domain.Operation{}, domain.E("batch_limit", "A batch can create at most 250 items, including nested project items.", 2)
	}

	if !b.DryRun {
		if err := s.checkWriteDatabase(); err != nil {
			return domain.Operation{}, err
		}
	}

	// Check authorization for the entire batch before any child is dispatched.
	if !b.DryRun && len(updates) > 0 {
		if s.Tokens == nil {
			return domain.Operation{}, domain.E("token_missing", "Run things auth set before updating items.", 4)
		}
		if _, err := s.Tokens.Token(ctx); err != nil {
			return domain.Operation{}, err
		}
	}

	id, err := newID()
	if err != nil {
		return domain.Operation{}, err
	}
	now := s.now().UTC()
	parent := domain.Operation{
		ID:          id,
		Database:    database,
		RequestKey:  b.OperationKey,
		RequestHash: canonicalHash,
		InputHash:   hash,
		Request:     domain.Request{Action: "batch"},
		State:       "prepared",
		Outcome:     "unknown",
		ItemIDs:     []string{},
		Evidence:    []domain.Evidence{},
		Errors:      []*domain.Error{},
		Children:    []domain.Operation{},
		CreatedAt:   now,
		UpdatedAt:   now,
		DryRun:      b.DryRun,
	}
	for n, r := range plans {
		cid, err := newID()
		if err != nil {
			return parent, err
		}
		child := domain.Operation{
			ID:          cid,
			Database:    database,
			RequestHash: requestHash(r),
			InputHash:   requestHash(b.Actions[n]),
			Request:     r,
			State:       "prepared",
			Outcome:     "unknown",
			ItemIDs:     []string{},
			Evidence:    []domain.Evidence{},
			Errors:      []*domain.Error{},
			CreatedAt:   now,
			UpdatedAt:   now,
			DryRun:      b.DryRun,
		}
		parent.Children = append(parent.Children, child)
	}
	if b.DryRun {
		return parent, nil
	}

	if err := s.Journal.Save(ctx, parent); err != nil {
		return parent, err
	}
	for _, child := range parent.Children {
		if err := s.Journal.Save(ctx, child); err != nil {
			return parent, err
		}
	}
	parent.State = "dispatching"
	if err := s.Journal.Save(ctx, parent); err != nil {
		return parent, err
	}

	stopped := false
	for n, child := range parent.Children {
		if stopped {
			child.State = "failed"
			child.Outcome = "failed"
			child.Errors = append(child.Errors, domain.E("not_dispatched", "This action was not dispatched because an earlier action did not confirm.", 6))
			child, err = s.persist(ctx, child)
		} else {
			input := child.Request
			// Revalidate a reminder's inherited schedule at execution time.
			if b.Actions[n].Patch.When == nil && b.Actions[n].Patch.Reminder != nil {
				input.Patch.When = nil
			}
			child, err = s.mutateLocked(ctx, input, child.InputHash, &child)
			if err != nil {
				if child.ID == "" {
					child = parent.Children[n]
					child.State = "failed"
					child.Outcome = "failed"
				} else if child.State == "dispatching" || child.State == "acknowledged" {
					child.State = "unknown"
					child.Outcome = "unknown"
				}
				child.Errors = append(child.Errors, domain.AsError(err))
				child, err = s.persist(ctx, child)
			}
		}
		if err != nil {
			return parent, err
		}

		parent.Children[n] = child
		if child.Outcome != "confirmed" {
			stopped = true
		}
		aggregate(&parent)
		if _, err := s.persist(ctx, parent); err != nil {
			return parent, err
		}
	}

	aggregate(&parent)
	return s.persist(ctx, parent)
}

func aggregate(o *domain.Operation) {
	confirmed, failed, unknown := 0, 0, 0
	o.ItemIDs = []string{}
	for _, c := range o.Children {
		switch c.Outcome {
		case "confirmed":
			confirmed++
		case "failed":
			failed++
		default:
			unknown++
		}
		o.ItemIDs = append(o.ItemIDs, c.ItemIDs...)
	}

	switch {
	case confirmed == len(o.Children):
		o.Outcome = "confirmed"
		o.State = "verified"
	case failed == len(o.Children):
		o.Outcome = "failed"
		o.State = "failed"
	case unknown == len(o.Children):
		o.Outcome = "unknown"
		o.State = "unknown"
	default:
		o.Outcome = "partial"
		o.State = "unknown"
	}
}

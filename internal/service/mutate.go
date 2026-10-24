package service

import (
	"context"
	"time"

	"github.com/mattt/things-cli/internal/callback"
	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/keychain"
	"github.com/mattt/things-cli/internal/urlscheme"
)

func (s *Service) Mutate(ctx context.Context, input domain.Request) (domain.Operation, error) {
	if s.Journal == nil {
		return domain.Operation{}, domain.E("journal_required", "A durable journal is required for mutations.", 4)
	}

	unlock, err := s.Journal.Lock(ctx)
	if err != nil {
		return domain.Operation{}, err
	}
	defer unlock()

	hash := requestHash(input)
	if input.OperationKey != "" && !input.DryRun {
		o, err := s.Journal.ByKey(ctx, input.OperationKey)
		if err == nil {
			if o.InputHash != hash {
				normalized, _, _, err := s.prepare(ctx, input)
				if err != nil || o.RequestHash != requestHash(normalized) {
					return domain.Operation{}, domain.E("operation_key_conflict", "This operation key belongs to a different request.", 2)
				}
			}
			if o.Outcome == "unknown" {
				o, err = s.reconcile(ctx, o)
			}
			return o, err
		}
		if domain.AsError(err).Code != "operation_missing" {
			return domain.Operation{}, err
		}
	}
	return s.mutateLocked(ctx, input, hash, nil)
}

func (s *Service) mutateLocked(ctx context.Context, input domain.Request, hash string, reserved *domain.Operation) (domain.Operation, error) {
	r, snap, before, err := s.prepare(ctx, input)
	if err != nil {
		return domain.Operation{}, err
	}

	if reserved != nil {
		if err := checkOperationDatabase(*reserved, snap); err != nil {
			return domain.Operation{}, err
		}
	}
	if !input.DryRun {
		if err := s.checkWriteDatabase(); err != nil {
			return domain.Operation{}, err
		}
	}
	id, err := newID()
	if err != nil {
		return domain.Operation{}, err
	}
	now := s.now().UTC()
	o := domain.Operation{
		ID:             id,
		Database:       snap.Database,
		SchedulingDate: now.In(time.Local).Format("2006-01-02"),
		RequestKey:     input.OperationKey,
		RequestHash:    requestHash(r),
		InputHash:      hash,
		Request:        r,
		State:          "prepared",
		Outcome:        "unknown",
		ItemIDs:        []string{},
		Evidence:       []domain.Evidence{},
		Errors:         []*domain.Error{},
		CreatedAt:      now,
		UpdatedAt:      now,
		DryRun:         input.DryRun,
	}
	if before != nil && before.Evening && input.Patch.When == nil && input.Patch.Reminder != nil {
		o.SchedulingDate = before.StartDate
	}
	if reserved != nil {
		o.ID = reserved.ID
		o.CreatedAt = reserved.CreatedAt
	}
	if before != nil {
		o.BeforeFingerprint = before.Fingerprint
		o.ItemIDs = []string{before.ID}
	} else {
		for _, i := range snap.Items {
			o.BaselineIDs = append(o.BaselineIDs, i.ID)
		}
	}

	if input.DryRun {
		o.BaselineIDs = nil
		return o, nil
	}

	noop := before != nil && matchPatch(*before, r.Patch, o.SchedulingDate)
	if noop {
		o.State = "verified"
		o.Outcome = "confirmed"
		o.Evidence = append(o.Evidence, domain.Evidence{
			Source:     "state_observation",
			ObservedAt: snap.ObservedAt,
			Detail:     "The requested postcondition already holds; no URL was dispatched.",
		})
		if err := s.Journal.Save(ctx, o); err != nil {
			return o, err
		}
		return o, nil
	}

	token := ""
	if before != nil {
		if s.Tokens == nil {
			return o, keychain.ErrMissing
		}
		token, err = s.Tokens.Token(ctx)
		if err != nil {
			return domain.Operation{}, err
		}
	}
	if err := s.Journal.Save(ctx, o); err != nil {
		return o, err
	}

	// Recheck immediately before dispatch; URL updates do not provide compare-and-swap.
	if before != nil {
		fresh, err := s.Reader.Snapshot(ctx)
		if err != nil {
			return s.failPrepared(ctx, o, domain.AsError(err))
		}
		current, err := resolve(fresh.Items, before.ID, "", false)
		if err != nil {
			return s.failPrepared(ctx, o, domain.AsError(err))
		}
		if err := checkOperationDatabase(o, fresh); err != nil {
			return s.failPrepared(ctx, o, domain.AsError(err))
		}
		if current.Fingerprint != before.Fingerprint {
			return s.failPrepared(ctx, o, domain.E("fingerprint_conflict", "The item changed during preflight. No URL was dispatched.", 6))
		}
	}

	if err := s.checkWriteDatabase(); err != nil {
		return s.failPrepared(ctx, o, domain.AsError(err))
	}
	count := 0
	if before == nil {
		count = 1 + len(r.Items)
	}
	if err := s.Journal.ReserveRate(ctx, count); err != nil {
		return s.failPrepared(ctx, o, domain.AsError(err))
	}

	callbacks, closeCallbacks, err := s.startCallbacks(o.ID)
	if err != nil {
		return s.failPrepared(ctx, o, domain.AsError(err))
	}
	defer closeCallbacks()

	value, err := urlscheme.Build(dispatchRequest(r, snap.Items), token, callbacks.URLs)
	if err != nil {
		return s.failPrepared(ctx, o, domain.AsError(err))
	}
	if len(value) > 200000 {
		return s.failPrepared(ctx, o, domain.E("url_size_limit", "The encoded request is too large for macOS URL dispatch. Use smaller operations.", 2))
	}

	o.State = "dispatching"
	o.UpdatedAt = s.now().UTC()
	if err := s.Journal.Save(ctx, o); err != nil {
		return o, err
	}

	if hasEvening(r) && o.SchedulingDate != s.now().In(time.Local).Format("2006-01-02") {
		return s.failPrepared(ctx, o, domain.E("schedule_date_changed", "The local date changed before Evening dispatch. No write was sent; submit a new operation.", 6))
	}
	callbacks.activate()
	if s.Dispatcher == nil {
		s.Dispatcher = urlscheme.System{}
	}
	dispatchErr := s.Dispatcher.Dispatch(ctx, value)
	if dispatchErr != nil {
		o.Errors = append(o.Errors, domain.E("dispatch_uncertain", "URL dispatch did not return a reliable acknowledgment.", 7))
	}

	timeout := s.VerifyTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	interval := s.PollInterval
	if interval == 0 {
		interval = 100 * time.Millisecond
	}
	verifyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		if cb := callbacks.take(); cb != nil && (cb.Outcome != "success" || len(cb.IDs) == 1 && (before == nil || cb.IDs[0] == before.ID)) {
			o.Evidence = append(o.Evidence, domain.Evidence{
				Source:     "callback_acknowledgment",
				ObservedAt: s.now().UTC(),
				Detail:     cb.Outcome,
			})
			if cb.Outcome != "success" {
				o.State = "failed"
				o.Outcome = "failed"
				o.Errors = append(o.Errors, domain.E("write_rejected", "Things rejected or canceled this operation.", 6))
				return s.persist(ctx, o)
			}

			o.State = "acknowledged"
			if len(cb.IDs) > 0 {
				o.ItemIDs = cb.IDs
			}
			if err := s.Journal.Save(ctx, o); err != nil {
				return o, err
			}
		}

		fresh, readErr := s.Reader.Snapshot(verifyCtx)
		if readErr == nil {
			readErr = checkOperationDatabase(o, fresh)
		}
		if readErr == nil && s.observe(&o, fresh) {
			return s.persist(ctx, o)
		}

		select {
		case <-verifyCtx.Done():
			o.State = "unknown"
			o.Outcome = "unknown"
			if readErr != nil {
				o.Errors = append(o.Errors, domain.E("verification_read_failed", "Read-back could not establish the requested state.", 7))
			}
			return s.persist(ctx, o)
		case <-time.After(interval):
		}
	}
}

func (s *Service) failPrepared(ctx context.Context, o domain.Operation, e *domain.Error) (domain.Operation, error) {
	o.State = "failed"
	o.Outcome = "failed"
	o.Errors = append(o.Errors, e)
	return s.persist(ctx, o)
}

func (s *Service) persist(ctx context.Context, o domain.Operation) (domain.Operation, error) {
	o.UpdatedAt = s.now().UTC()
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.Journal.Save(persistCtx, o); err != nil {
		return o, err
	}
	return o, nil
}

type callbackState struct {
	URLs     map[string]string
	listener *callback.Listener
}

func (s *Service) startCallbacks(id string) (*callbackState, func(), error) {
	if !s.Callbacks {
		return &callbackState{}, func() {}, nil
	}
	l, err := callback.Start(id)
	if err != nil {
		return nil, nil, err
	}
	return &callbackState{l.URLs, l}, l.Close, nil
}

func (c *callbackState) take() *callback.Result { return c.listener.Take() }

func (c *callbackState) activate() {
	if c.listener != nil {
		c.listener.Activate()
	}
}

func hasEvening(r domain.Request) bool {
	if r.Patch.When != nil && *r.Patch.When == "evening" {
		return true
	}
	for _, child := range r.Items {
		if child.Patch.When != nil && *child.Patch.When == "evening" {
			return true
		}
	}
	return false
}

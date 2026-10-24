package service

import (
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/callback"
	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/journal"
	"github.com/mattt/things-cli/internal/keychain"
	"github.com/mattt/things-cli/internal/readstore"
	"github.com/mattt/things-cli/internal/testutil"
	"github.com/mattt/things-cli/internal/urlscheme"
)

type memoryReader struct {
	mu       sync.Mutex
	items    []domain.Item
	err      error
	database string
	reads    int
	onRead   func(int, []domain.Item)
}

func TestCallbacksRecordRejectionAndAcknowledgment(t *testing.T) {
	for _, outcome := range []string{"error", "cancel", "success"} {
		t.Run(outcome, func(t *testing.T) {
			s, m := testService(t, todo("task", "Before"))
			s.Callbacks = true
			s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
				u, _ := url.Parse(raw)
				q := u.Query()
				callbackURL := q.Get("x-" + outcome)
				if callbackURL == "" {
					t.Fatal("callback URL missing")
				}

				c, _ := url.Parse(callbackURL)
				if c.Query().Has("auth-token") || c.Query().Has("notes") {
					t.Fatal("sensitive callback content")
				}

				if outcome == "success" {
					callbackURL += "&x-things-id=task"
				}
				if err := callback.Forward(callbackURL); err != nil {
					t.Fatal(err)
				}

				if outcome == "success" {
					time.Sleep(5 * time.Millisecond)
					m.mu.Lock()
					m.items[0].Title = "After"
					m.mu.Unlock()
				}
				return nil
			})

			o, err := s.Mutate(context.Background(), domain.Request{
				Action: "update",
				ID:     "task",
				Patch:  domain.Patch{Title: str("After")},
			})
			if err != nil {
				t.Fatal(err)
			}

			want := "failed"
			if outcome == "success" {
				want = "confirmed"
			}
			if o.Outcome != want {
				t.Fatal(o)
			}

			ack := false
			for _, e := range o.Evidence {
				if e.Source == "callback_acknowledgment" {
					ack = true
				}
			}
			if !ack {
				t.Fatal("callback evidence missing")
			}

			saved, err := s.Journal.Get(context.Background(), o.ID)
			if err != nil || saved.Outcome != o.Outcome {
				t.Fatal("callback outcome was not durable", saved, err)
			}
		})
	}
}

func (m *memoryReader) Snapshot(context.Context) (readstore.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reads++
	if m.onRead != nil {
		m.onRead(m.reads, m.items)
	}

	copy := append([]domain.Item{}, m.items...)
	for n := range copy {
		copy[n].Fingerprint = domain.Fingerprint(copy[n])
	}
	return readstore.Snapshot{Items: copy, Database: m.database, ObservedAt: time.Now().UTC(), Version: 24}, m.err
}

type dispatchFunc func(context.Context, string) error

func (d dispatchFunc) Dispatch(ctx context.Context, value string) error { return d(ctx, value) }

type token string

func (t token) Token(context.Context) (string, error) {
	if t == "" {
		return "", keychain.ErrMissing
	}
	return string(t), nil
}

func testService(t *testing.T, items ...domain.Item) (*Service, *memoryReader) {
	t.Helper()
	m := &memoryReader{items: items}
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })

	return &Service{
		Reader:        m,
		Journal:       j,
		Tokens:        token("secret"),
		VerifyTimeout: 25 * time.Millisecond,
		PollInterval:  time.Millisecond,
		Now:           func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local) },
	}, m
}

func todo(id, title string) domain.Item {
	return domain.Item{
		ID:        id,
		Kind:      "todo",
		Title:     title,
		Status:    "incomplete",
		Start:     "anytime",
		Tags:      []domain.Tag{},
		Checklist: []domain.ChecklistItem{},
	}
}

func TestConfirmedUpdateAndNoop(t *testing.T) {
	s, m := testService(t, todo("task", "Before"))
	var calls int
	s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
		calls++
		u, _ := url.Parse(raw)
		if u.Query().Get("auth-token") != "secret" {
			t.Fatal("missing auth")
		}

		m.mu.Lock()
		m.items[0].Title = u.Query().Get("title")
		m.mu.Unlock()
		return nil
	})

	request := domain.Request{
		Action:       "update",
		ID:           "task",
		Patch:        domain.Patch{Title: str("After")},
		OperationKey: "once",
	}
	o, err := s.Mutate(context.Background(), request)
	if err != nil || o.Outcome != "confirmed" || calls != 1 {
		t.Fatal(o, err, calls)
	}

	saved, err := s.Mutate(context.Background(), request)
	if err != nil || saved.ID != o.ID || calls != 1 {
		t.Fatal("operation replayed", saved, err)
	}

	alias := request
	alias.ID = "tas"
	saved, err = s.Mutate(context.Background(), alias)
	if err != nil || saved.ID != o.ID || calls != 1 {
		t.Fatal("normalized selector lost replay protection", saved, err)
	}

	request.OperationKey = "noop"
	o, err = s.Mutate(context.Background(), request)
	if err != nil || o.Outcome != "confirmed" || calls != 1 {
		t.Fatal("noop dispatched", o, err)
	}

	request.Patch.Title = str("Different")
	request.OperationKey = "once"
	_, err = s.Mutate(context.Background(), request)
	if domain.AsError(err).Code != "operation_key_conflict" {
		t.Fatal(err)
	}
}

func TestUnknownUpdateNeverReplaysAndCanReconcile(t *testing.T) {
	s, m := testService(t, todo("task", "Before"))
	calls := 0
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		calls++
		return nil
	})

	r := domain.Request{
		Action:       "update",
		ID:           "task",
		Patch:        domain.Patch{Title: str("After")},
		OperationKey: "unknown",
	}
	o, err := s.Mutate(context.Background(), r)
	if err != nil || o.Outcome != "unknown" || domain.AsError(o.ResultError()).Exit != 7 {
		t.Fatal(o, err)
	}

	_, err = s.Mutate(context.Background(), r)
	if err != nil || calls != 1 {
		t.Fatal("uncertain write replayed", err, calls)
	}

	m.mu.Lock()
	m.items[0].Title = "After"
	m.mu.Unlock()
	o, err = s.Operation(context.Background(), o.ID, true)
	if err != nil || o.Outcome != "confirmed" || calls != 1 {
		t.Fatal(o, err)
	}
}

func TestCreateOnlyNewUniqueCandidates(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			s, m := testService(t, todo("existing", "Same"))
			s.Dispatcher = dispatchFunc(func(context.Context, string) error {
				m.mu.Lock()
				defer m.mu.Unlock()
				for n := 0; n < count; n++ {
					m.items = append(m.items, todo(string(rune('a'+n)), "Same"))
				}
				return nil
			})

			o, err := s.Mutate(context.Background(), domain.Request{
				Action: "create_todo",
				Patch:  domain.Patch{Title: str("Same")},
			})
			if err != nil {
				t.Fatal(err)
			}

			want := "unknown"
			if count == 1 {
				want = "confirmed"
			}
			if o.Outcome != want {
				t.Fatalf("%d candidates: %+v", count, o)
			}
			if count == 1 && o.ItemIDs[0] == "existing" {
				t.Fatal("preexisting title confirmed creation")
			}
		})
	}
}

func TestValidationNoDispatch(t *testing.T) {
	tag := domain.Item{ID: "tag", Kind: "tag", Title: "work"}
	heading := domain.Item{ID: "heading", Kind: "heading", ProjectID: "other", Title: "Heading"}
	project := domain.Item{ID: "project", Kind: "project", Title: "Project", Status: "incomplete"}
	s, _ := testService(t, todo("task-a", "Duplicate"), todo("task-b", "Duplicate"), tag, heading, project)
	calls := 0
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		calls++
		return nil
	})

	cases := []domain.Request{
		{Action: "update", ID: "task", Patch: domain.Patch{Notes: str("text")}},
		{Action: "update", ID: "Duplicate", Patch: domain.Patch{Notes: str("text")}},
		{Action: "update", ID: "tag", Patch: domain.Patch{Notes: str("text")}},
		{Action: "create_todo", Patch: domain.Patch{Title: str("T"), AddTags: []string{"missing"}}},
		{Action: "create_todo", Patch: domain.Patch{Title: str("T"), Reminder: str("12:30")}},
		{Action: "create_todo", Patch: domain.Patch{Title: str("T"), Project: str("project"), Heading: str("heading")}},
	}
	for _, r := range cases {
		if _, err := s.Mutate(context.Background(), r); err == nil {
			t.Fatalf("accepted invalid request: %+v", r)
		}
	}

	if calls != 0 {
		t.Fatal("invalid request dispatched")
	}
}

func TestAreaMoveIsNotInheritedAreaNoop(t *testing.T) {
	i := todo("task", "Task")
	i.ProjectID = "project"
	i.AreaID = "area"
	s, m := testService(t, i, domain.Item{ID: "area", Kind: "area", Title: "Work"})
	calls := 0
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		calls++
		m.mu.Lock()
		m.items[0].ProjectID = ""
		m.items[0].HeadingID = ""
		m.mu.Unlock()
		return nil
	})

	o, err := s.Mutate(context.Background(), domain.Request{
		Action: "update",
		ID:     "task",
		Patch:  domain.Patch{Area: str("Work")},
	})
	if err != nil || o.Outcome != "confirmed" || calls != 1 {
		t.Fatal("area move became false no-op", o, err, calls)
	}
}

func TestNestedProjectReadbackAndIDs(t *testing.T) {
	s, m := testService(t)
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		p := domain.Item{ID: "project", Kind: "project", Title: "Project", Status: "incomplete"}
		h := domain.Item{ID: "heading", Kind: "heading", Title: "Heading", ProjectID: p.ID}
		a := todo("a", "Same")
		a.ProjectID = p.ID
		a.HeadingID = h.ID
		a.Index = 0
		b := a
		b.ID = "b"
		b.Index = 1

		m.mu.Lock()
		m.items = []domain.Item{p, h, a, b}
		m.mu.Unlock()
		return nil
	})

	r := domain.Request{
		Action: "create_project",
		Patch:  domain.Patch{Title: str("Project")},
		Items: []domain.ProjectChild{
			{Kind: "heading", Patch: domain.Patch{Title: str("Heading")}},
			{Kind: "todo", Patch: domain.Patch{Title: str("Same")}},
			{Kind: "todo", Patch: domain.Patch{Title: str("Same")}},
		},
	}
	o, err := s.Mutate(context.Background(), r)
	if err != nil || o.Outcome != "confirmed" || len(o.ItemIDs) != 4 || o.ItemIDs[0] != "project" {
		t.Fatal(o, err)
	}
}

func TestPreparedCrashCannotConfirmUnsentCreation(t *testing.T) {
	s, _ := testService(t, todo("candidate", "New"))
	o := domain.Operation{
		ID:          "prepared",
		RequestHash: "hash",
		State:       "prepared",
		Outcome:     "unknown",
		Request:     domain.Request{Action: "create_todo", Patch: domain.Patch{Title: str("New")}},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := s.Journal.Save(context.Background(), o); err != nil {
		t.Fatal(err)
	}

	o, err := s.Operation(context.Background(), o.ID, true)
	if err != nil || o.Outcome != "failed" || domain.AsError(o.ResultError()).Code != "not_dispatched" {
		t.Fatal(o, err)
	}
}

func TestConflictMissingTokenDryRunAndCrash(t *testing.T) {
	s, m := testService(t, todo("task", "Before"))
	calls := 0
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		calls++
		return nil
	})

	r := domain.Request{
		Action:              "update",
		ID:                  "task",
		Patch:               domain.Patch{Title: str("After")},
		ExpectedFingerprint: "bad",
	}
	if _, err := s.Mutate(context.Background(), r); domain.AsError(err).Code != "fingerprint_conflict" {
		t.Fatal(err)
	}

	r.ExpectedFingerprint = ""
	s.Tokens = token("")
	if _, err := s.Mutate(context.Background(), r); domain.AsError(err).Code != "token_missing" {
		t.Fatal(err)
	}

	r.DryRun = true
	r.OperationKey = "dry"
	o, err := s.Mutate(context.Background(), r)
	if err != nil || !o.DryRun {
		t.Fatal(o, err)
	}
	if _, err := s.Journal.ByKey(context.Background(), "dry"); domain.AsError(err).Code != "operation_missing" {
		t.Fatal("dry-run reserved key")
	}

	s.Tokens = token("secret")
	r.DryRun = false
	r.OperationKey = ""
	m.onRead = func(n int, items []domain.Item) {
		if n == 5 {
			items[0].Title = "Concurrent edit"
		}
	}
	_, _ = s.Mutate(context.Background(), r)
	if calls != 0 {
		t.Fatal("changed item dispatched")
	}

	crash := domain.Operation{
		ID:        "crash",
		Request:   domain.Request{Action: "update", ID: "task", Patch: domain.Patch{Title: str("After")}},
		State:     "dispatching",
		Outcome:   "unknown",
		ItemIDs:   []string{"task"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.Journal.Save(context.Background(), crash); err != nil {
		t.Fatal(err)
	}

	o, err = s.Operation(context.Background(), "crash", true)
	if err != nil || o.Outcome != "unknown" || calls != 0 {
		t.Fatal(o, err)
	}
}

func TestBatchPreflightAndPartialRecovery(t *testing.T) {
	s, m := testService(t, todo("task", "Before"))
	calls := 0
	s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
		calls++
		u, _ := url.Parse(raw)
		if u.Path == "/add" {
			m.mu.Lock()
			m.items = append(m.items, todo("new", "New"))
			m.mu.Unlock()
		}
		return nil
	})

	bad := domain.BatchRequest{
		Actions: []domain.Request{
			{Action: "create_todo", Patch: domain.Patch{Title: str("New")}},
			{Action: "update", ID: "missing", Patch: domain.Patch{Title: str("After")}},
		},
	}
	if _, err := s.Batch(context.Background(), bad); err == nil || calls != 0 {
		t.Fatal("batch failed to preflight", err, calls)
	}

	bad.Actions[1].ID = "task"
	bad.OperationKey = "batch"
	o, err := s.Batch(context.Background(), bad)
	if err != nil || o.Outcome != "partial" || len(o.Children) != 2 || o.Children[0].Outcome != "confirmed" || o.Children[1].Outcome != "unknown" {
		t.Fatal(o, err)
	}

	again, err := s.Batch(context.Background(), bad)
	if err != nil || again.ID != o.ID || calls != 2 || again.Children[0].ID != o.Children[0].ID {
		t.Fatal("batch replayed or changed identities", again, err, calls)
	}
}

func TestTwoServicesSerializeSameOperationKey(t *testing.T) {
	dir := t.TempDir()
	j1, err := journal.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j1.Close()

	j2, err := journal.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()

	m := &memoryReader{items: []domain.Item{todo("task", "Before")}}
	var calls atomic.Int32
	d := dispatchFunc(func(context.Context, string) error {
		calls.Add(1)
		m.mu.Lock()
		m.items[0].Title = "After"
		m.mu.Unlock()
		return nil
	})
	s1 := &Service{Reader: m, Journal: j1, Tokens: token("secret"), Dispatcher: d}
	s2 := &Service{Reader: m, Journal: j2, Tokens: token("secret"), Dispatcher: d}
	r := domain.Request{
		Action:       "update",
		ID:           "task",
		Patch:        domain.Patch{Title: str("After")},
		OperationKey: "shared",
	}

	var wg sync.WaitGroup
	for _, s := range []*Service{s1, s2} {
		wg.Go(func() {
			o, err := s.Mutate(context.Background(), r)
			if err != nil || o.Outcome != "confirmed" {
				t.Error(o, err)
			}
		})
	}
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatal("concurrent write replayed", calls.Load())
	}
}

func BenchmarkList10000(b *testing.B) {
	db, path := testutil.Database(b)
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}

	stmt, err := tx.Prepare("INSERT INTO TMTask(uuid,title,start,startDate) VALUES(?,?,1,?)")
	if err != nil {
		b.Fatal(err)
	}
	for n := 0; n < 10000; n++ {
		if _, err := stmt.Exec(time.Unix(int64(n), 0).Format(time.RFC3339), "Task", testutil.Date("2026-10-08")); err != nil {
			b.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}

	s := &Service{Reader: readstore.Lazy{Path: path}}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		r, err := s.List(context.Background(), Query{List: "today", Limit: 50})
		if err != nil || len(r.Data.([]domain.Item)) != 50 {
			b.Fatal(r, err)
		}
	}
}

func TestTagIdentityAndDuplicateTitles(t *testing.T) {
	ctx := context.Background()
	task := todo("task", "Task")
	task.Tags = []domain.Tag{{ID: "a", Title: "work"}}
	for _, addition := range []bool{false, true} {
		t.Run(map[bool]string{false: "replace", true: "add"}[addition], func(t *testing.T) {
			s, m := testService(t, task, domain.Item{ID: "a", Kind: "tag", Title: "work"}, domain.Item{ID: "b", Kind: "tag", Title: "work"})
			tags := []string{"b"}
			patch := domain.Patch{Tags: &tags}
			if addition {
				patch = domain.Patch{AddTags: tags}
			}
			s.Dispatcher = dispatchFunc(func(context.Context, string) error { t.Fatal("dispatched ambiguous tag"); return nil })
			if _, err := s.Mutate(ctx, domain.Request{Action: "update", ID: "task", Patch: patch}); err == nil || domain.AsError(err).Code != "ambiguous_tag_title" {
				t.Fatal(err)
			}
			m.items = m.items[:2]
			tags = []string{"a"}
			r, _, _, err := s.prepare(ctx, domain.Request{Action: "update", ID: "task", Patch: domain.Patch{Tags: &tags}})
			if err != nil || (*r.Patch.Tags)[0] != "a" {
				t.Fatal(r, err)
			}
			if !matchPatch(task, r.Patch, s.now().Format("2006-01-02")) {
				t.Fatal("same identity must match")
			}
			task.Tags[0].ID = "b"
			if matchPatch(task, r.Patch, s.now().Format("2006-01-02")) {
				t.Fatal("different identity matched")
			}
			task.Tags[0].ID = "a"
			raw, err := urlscheme.Build(dispatchRequest(r, m.items), "secret", nil)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(raw)
			if u.Query().Get("tags") != "work" {
				t.Fatal(raw)
			}
		})
	}
}

func TestReconcileRejectsDifferentDatabase(t *testing.T) {
	s, m := testService(t, todo("task", "Task"))
	m.database = "library-a"
	o := domain.Operation{ID: "database-operation", Database: "library-a", Request: domain.Request{Action: "update", ID: "task", Patch: domain.Patch{Title: str("Task")}}, State: "dispatching", Outcome: "unknown"}
	if err := s.Journal.Save(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	m.database = "library-b"
	got, err := s.Operation(context.Background(), o.ID, true)
	if err == nil || domain.AsError(err).Code != "operation_database_mismatch" || got.Outcome != "unknown" {
		t.Fatal(got, err)
	}
	m.database = "library-a"
	got, err = s.Operation(context.Background(), o.ID, true)
	if err != nil || got.Outcome != "confirmed" {
		t.Fatal(got, err)
	}
}

func TestBatchRecoveryAfterCrashAndPrune(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	old := s.now().Add(-91 * 24 * time.Hour)
	child := domain.Operation{ID: "crash-child", State: "prepared", Outcome: "unknown", CreatedAt: old, UpdatedAt: old}
	parent := domain.Operation{ID: "crash-parent", Request: domain.Request{Action: "batch"}, State: "dispatching", Outcome: "unknown", Children: []domain.Operation{child}, CreatedAt: old, UpdatedAt: old}
	if err := s.Journal.Save(ctx, parent); err != nil {
		t.Fatal(err)
	}
	child.State, child.Outcome = "verified", "confirmed"
	child.ItemIDs = []string{"created-task"}
	if err := s.Journal.Save(ctx, child); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Journal.Prune(ctx, s.now()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	got, err := s.Operation(ctx, parent.ID, true)
	if err != nil || got.Outcome != "confirmed" || got.Children[0].Outcome != "confirmed" || got.ItemIDs[0] != "created-task" {
		t.Fatal(got, err)
	}
}

func TestBatchRecoveryStopsOnChildReadError(t *testing.T) {
	s, _ := testService(t)
	parent := domain.Operation{ID: "parent", Request: domain.Request{Action: "batch"}, State: "dispatching", Outcome: "unknown", Children: []domain.Operation{{ID: "missing-child", State: "prepared", Outcome: "unknown"}}}
	if _, err := s.reconcile(context.Background(), parent); err == nil {
		t.Fatal("missing child was treated as not dispatched")
	}
	if err := s.Journal.Close(); err != nil {
		t.Fatal(err)
	}
	parent.State = "prepared"
	if _, err := s.reconcile(context.Background(), parent); err == nil {
		t.Fatal("child read error was ignored")
	}
}

func TestEveningBatchChildAcrossMidnight(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "applied"}[apply], func(t *testing.T) {
			item := todo("task", "Task")
			item.StartDate, item.Evening = "2026-10-08", true
			s, m := testService(t, item)
			previous := s.now()
			current := previous.AddDate(0, 0, 1)
			s.Now = func() time.Time { return current }
			dispatched := false
			s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
				dispatched = true
				u, _ := url.Parse(raw)
				if u.Query().Get("when") != "evening" {
					t.Fatal(raw)
				}
				if apply {
					m.mu.Lock()
					m.items[0].StartDate = "2026-10-09"
					m.mu.Unlock()
				}
				// Verification may run after the dispatch date.
				current = current.AddDate(0, 0, 1)
				return nil
			})
			reserved := domain.Operation{ID: "reserved-child", CreatedAt: previous}
			o, err := s.mutateLocked(context.Background(), domain.Request{Action: "update", ID: "task", Patch: domain.Patch{When: str("evening")}}, "hash", &reserved)
			want := "unknown"
			if apply {
				want = "confirmed"
			}
			if err != nil || !dispatched || o.Outcome != want || o.SchedulingDate != "2026-10-09" || !o.CreatedAt.Equal(previous) {
				t.Fatal(o, err, dispatched)
			}
		})
	}
}

func TestEveningDispatchRejectsDateChange(t *testing.T) {
	s, m := testService(t, todo("task", "Task"))
	now := s.now()
	s.Now = func() time.Time { return now }
	m.onRead = func(n int, _ []domain.Item) {
		if n == 2 {
			now = now.AddDate(0, 0, 1)
		}
	}
	s.Dispatcher = dispatchFunc(func(context.Context, string) error { t.Fatal("dispatched after date changed"); return nil })
	o, err := s.Mutate(context.Background(), domain.Request{Action: "update", ID: "task", Patch: domain.Patch{When: str("evening")}})
	if err != nil || o.Outcome != "failed" || o.Errors[0].Code != "schedule_date_changed" {
		t.Fatal(o, err)
	}
}

func TestReminderOnlyPreservesEveningDate(t *testing.T) {
	for _, date := range []string{"2026-10-07", "2026-10-08", "2026-10-09"} {
		for _, reminder := range []string{"", "18:00"} {
			t.Run(date+"/"+reminder, func(t *testing.T) {
				item := todo("task", "Task")
				item.StartDate, item.Evening, item.Reminder = date, true, "17:00"
				s, m := testService(t, item)
				dispatched := false
				s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
					dispatched = true
					u, _ := url.Parse(raw)
					want := "evening"
					if reminder != "" {
						want += "@" + reminder
					}
					if u.Query().Get("when") != want {
						t.Fatal(raw)
					}
					m.mu.Lock()
					m.items[0].Reminder = reminder
					m.mu.Unlock()
					return nil
				})
				o, err := s.Mutate(context.Background(), domain.Request{Action: "update", ID: "task", Patch: domain.Patch{Reminder: str(reminder)}})
				if date != "2026-10-08" {
					if err == nil || domain.AsError(err).Code != "evening_reminder_unsupported" || dispatched {
						t.Fatal(o, err, dispatched)
					}
				} else if err != nil || o.Outcome != "confirmed" || o.SchedulingDate != date {
					t.Fatal(o, err)
				}
				if m.items[0].StartDate != date || !m.items[0].Evening {
					t.Fatal("schedule changed")
				}
			})
		}
	}
}

func TestBatchReminderRevalidatesEveningDate(t *testing.T) {
	item := todo("evening-task", "Evening")
	item.StartDate, item.Evening, item.Reminder = "2026-10-08", true, "17:00"
	s, m := testService(t, todo("first", "First"), item)
	now := s.now()
	s.Now = func() time.Time { return now }
	dispatches := 0
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		dispatches++
		if dispatches > 1 {
			t.Fatal("dispatched reminder after midnight")
		}
		m.mu.Lock()
		m.items[0].Title = "Changed"
		m.mu.Unlock()
		now = now.AddDate(0, 0, 1)
		return nil
	})
	o, err := s.Batch(context.Background(), domain.BatchRequest{Actions: []domain.Request{
		{Action: "update", ID: "first", Patch: domain.Patch{Title: str("Changed")}},
		{Action: "update", ID: "evening-task", Patch: domain.Patch{Reminder: str("")}},
	}})
	if err != nil || o.Outcome != "partial" || dispatches != 1 || o.Children[1].Errors[0].Code != "evening_reminder_unsupported" {
		t.Fatal(o, err)
	}
}

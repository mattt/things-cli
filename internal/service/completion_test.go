package service

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

func TestNormalizeCompletionDate(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	local := func(y int, m time.Month, d, h, min, s int) string {
		return time.Date(y, m, d, h, min, s, 0, time.Local).Format(time.RFC3339)
	}

	for _, c := range []struct {
		in, want, code string
	}{
		{in: "2026-10-01", want: local(2026, 10, 1, 0, 0, 0)},
		{in: "2026-10-01T09:30", want: local(2026, 10, 1, 9, 30, 0)},
		{in: "2026-10-01T09:30:15", want: local(2026, 10, 1, 9, 30, 15)},
		{in: "2026-10-01T09:30:15.75Z", want: time.Date(2026, 10, 1, 9, 30, 15, 0, time.UTC).In(time.Local).Format(time.RFC3339)},
		{in: "2026-10-01T09:30:00+02:00", want: time.Date(2026, 10, 1, 7, 30, 0, 0, time.UTC).In(time.Local).Format(time.RFC3339)},
		{in: "today", want: local(2026, 10, 8, 0, 0, 0)},
		{in: "yesterday", want: local(2026, 10, 7, 0, 0, 0)},
		{in: now.Format(time.RFC3339), want: now.Format(time.RFC3339)},
		{in: "2026-10-09", code: "future_completion_date"},
		{in: now.Add(time.Second).Format(time.RFC3339), code: "future_completion_date"},
		{in: "tomorrow", code: "invalid_completion_date"},
		{in: "10/01/2026", code: "invalid_completion_date"},
		{in: "2026-02-30", code: "invalid_completion_date"},
	} {
		got, err := normalizeCompletionDate(c.in, now)
		if c.code != "" {
			if domain.AsError(err).Code != c.code {
				t.Errorf("%s: got %q, %v; want %s", c.in, got, err, c.code)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %s", c.in, got, err, c.want)
		}
	}
}

func TestNormalizeCompletionDateDaylightSaving(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("time zone data unavailable:", err)
	}
	now := time.Date(2026, 12, 1, 12, 0, 0, 0, la)

	for _, c := range []struct {
		in, want, code string
	}{
		// 2026-03-08 02:00 PST jumps to 03:00 PDT.
		{in: "2026-03-08T02:30", code: "invalid_completion_date"},
		{in: "2026-03-08T02:30:00", code: "invalid_completion_date"},
		{in: "2026-03-08T01:59", want: "2026-03-08T01:59:00-08:00"},
		{in: "2026-03-08T03:00", want: "2026-03-08T03:00:00-07:00"},
		{in: "2026-03-08", want: "2026-03-08T00:00:00-08:00"},
		// 2026-11-01 02:00 PDT falls back to 01:00 PST; 01:30 repeats.
		{in: "2026-11-01T01:30", want: "2026-11-01T01:30:00-07:00"},
		{in: "2026-11-01T01:30:00-08:00", want: "2026-11-01T01:30:00-08:00"},
		{in: "2026-11-01T09:30:00Z", want: "2026-11-01T01:30:00-08:00"},
		{in: "2026-03-08T10:30:00Z", want: "2026-03-08T03:30:00-07:00"},
	} {
		got, err := normalizeCompletionDate(c.in, now)
		if c.code != "" {
			if domain.AsError(err).Code != c.code {
				t.Errorf("%s: got %q, %v; want %s", c.in, got, err, c.code)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %s", c.in, got, err, c.want)
		}
	}
}

func TestNormalizeCompletionDateSubsecondBoundary(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 100_000_000, time.UTC)
	got, err := normalizeCompletionDate("2026-10-08T12:00:00.1Z", now)
	if err != nil || got != "2026-10-08T12:00:00Z" {
		t.Fatal(got, err)
	}
	if _, err := normalizeCompletionDate("2026-10-08T12:00:00.9Z", now); domain.AsError(err).Code != "future_completion_date" {
		t.Fatal(err)
	}
}

func TestCompletionDateUpdateRequiresMatchingDate(t *testing.T) {
	stopped := time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local).UTC()
	for name, at := range map[string]*time.Time{"missing": nil, "mismatched": &stopped} {
		t.Run(name, func(t *testing.T) {
			i := todo("task", "Task")
			i.Status = "completed"
			i.StoppedAt = at
			s, _ := testService(t, i)
			calls := 0
			s.Dispatcher = dispatchFunc(func(context.Context, string) error {
				calls++
				return nil
			})

			o, err := s.Mutate(context.Background(), domain.Request{
				Action: "update",
				ID:     "task",
				Patch:  domain.Patch{CompletionDate: str("2026-10-01")},
			})
			if err != nil || calls != 1 || o.Outcome != "unknown" {
				t.Fatal(o, err, calls)
			}
		})
	}
}

func TestCompleteWithCompletionDate(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		t.Run(map[bool]string{false: "applied", true: "ignored"}[ignored], func(t *testing.T) {
			s, m := testService(t, todo("task", "Task"))
			var query url.Values
			s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
				u, _ := url.Parse(raw)
				query = u.Query()

				stopped := s.now().UTC()
				if !ignored {
					v, _ := time.Parse(time.RFC3339, query.Get("completion-date"))
					stopped = v.UTC()
				}
				m.mu.Lock()
				m.items[0].Status = "completed"
				m.items[0].StoppedAt = &stopped
				m.mu.Unlock()
				return nil
			})

			o, err := s.Mutate(context.Background(), domain.Request{
				Action: "set_status",
				ID:     "task",
				Patch:  domain.Patch{Status: str("completed"), CompletionDate: str("2026-10-01")},
			})
			if err != nil {
				t.Fatal(err)
			}

			want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local).Format(time.RFC3339)
			if query.Get("completed") != "true" || query.Get("completion-date") != want {
				t.Fatal(query)
			}
			outcome := "confirmed"
			if ignored {
				outcome = "unknown"
			}
			if o.Outcome != outcome || *o.Request.Patch.CompletionDate != want {
				t.Fatal(o)
			}
		})
	}
}

func TestCompletionDateNoop(t *testing.T) {
	i := todo("task", "Task")
	i.Status = "completed"
	stopped := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local).UTC().Add(250 * time.Millisecond)
	i.StoppedAt = &stopped
	s, _ := testService(t, i)
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		t.Fatal("noop dispatched")
		return nil
	})

	o, err := s.Mutate(context.Background(), domain.Request{
		Action: "update",
		ID:     "task",
		Patch:  domain.Patch{CompletionDate: str("2026-10-01")},
	})
	if err != nil || o.Outcome != "confirmed" {
		t.Fatal(o, err)
	}
}

func TestCreateClosedWithCompletionDate(t *testing.T) {
	s, m := testService(t)
	var query url.Values
	s.Dispatcher = dispatchFunc(func(_ context.Context, raw string) error {
		u, _ := url.Parse(raw)
		query = u.Query()

		v, _ := time.Parse(time.RFC3339, query.Get("completion-date"))
		v = v.UTC()
		i := todo("new", query.Get("title"))
		i.Status = "canceled"
		i.StoppedAt = &v
		m.mu.Lock()
		m.items = append(m.items, i)
		m.mu.Unlock()
		return nil
	})

	o, err := s.Mutate(context.Background(), domain.Request{
		Action: "create_todo",
		Patch:  domain.Patch{Title: str("Logged"), Status: str("canceled"), CompletionDate: str("2026-10-01T17:45:00Z")},
	})
	if err != nil || o.Outcome != "confirmed" || o.ItemIDs[0] != "new" {
		t.Fatal(o, err)
	}
	if query.Get("canceled") != "true" || query.Get("completion-date") != time.Date(2026, 10, 1, 17, 45, 0, 0, time.UTC).In(time.Local).Format(time.RFC3339) {
		t.Fatal(query)
	}
}

func TestCompletionDateValidation(t *testing.T) {
	template := todo("template", "Repeating")
	template.Recurrence = "template"
	template.Status = "completed"
	closed := todo("closed", "Closed")
	closed.Status = "completed"
	s, _ := testService(t, todo("open", "Open"), template, closed)
	s.Dispatcher = dispatchFunc(func(context.Context, string) error {
		t.Fatal("invalid request dispatched")
		return nil
	})

	for _, c := range []struct {
		r    domain.Request
		code string
	}{
		{domain.Request{Action: "set_status", ID: "open", Patch: domain.Patch{Status: str("completed"), CompletionDate: str("2026-10-09")}}, "future_completion_date"},
		{domain.Request{Action: "set_status", ID: "open", Patch: domain.Patch{Status: str("completed"), CompletionDate: str("last week")}}, "invalid_completion_date"},
		{domain.Request{Action: "set_status", ID: "closed", Patch: domain.Patch{Status: str("incomplete"), CompletionDate: str("2026-10-01")}}, "completion_date_requires_closed_status"},
		{domain.Request{Action: "update", ID: "open", Patch: domain.Patch{CompletionDate: str("2026-10-01")}}, "completion_date_requires_closed_status"},
		{domain.Request{Action: "update", ID: "closed", Patch: domain.Patch{CompletionDate: str("")}}, "completion_date_clear_unsupported"},
		{domain.Request{Action: "update", ID: "template", Patch: domain.Patch{CompletionDate: str("2026-10-01")}}, "recurrence_edit_unsupported"},
		{domain.Request{Action: "create_todo", Patch: domain.Patch{Title: str("T"), CompletionDate: str("2026-10-01")}}, "completion_date_requires_closed_status"},
		{domain.Request{Action: "set_status", ID: "closed", Patch: domain.Patch{Status: str("completed"), Title: str("T")}}, "invalid_status_patch"},
	} {
		c.r.DryRun = true
		if _, err := s.Mutate(context.Background(), c.r); domain.AsError(err).Code != c.code {
			t.Errorf("%+v: got %v, want %s", c.r, err, c.code)
		}
	}
}

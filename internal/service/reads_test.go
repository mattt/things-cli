package service

import (
	"context"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

func TestListsAndPagination(t *testing.T) {
	inbox := todo("inbox", "Inbox")
	inbox.Start = "inbox"

	today := todo("today", "Today")
	today.StartDate = "2026-10-08"
	today.TodayIndex = 1

	evening := todo("evening", "Evening")
	evening.StartDate = "2026-10-08"
	evening.Evening = true
	evening.TodayIndex = 2

	overdue := todo("overdue", "Overdue")
	overdue.Deadline = "2026-10-07"

	future := todo("future", "Future")
	future.Start = "someday"
	future.StartDate = "2026-10-09"

	someday := todo("someday", "Someday")
	someday.Start = "someday"

	completed := todo("completed", "Completed")
	completed.Status = "completed"
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	completed.StoppedAt = &at

	canceled := todo("canceled", "Canceled")
	canceled.Status = "canceled"
	at2 := at.Add(time.Hour)
	canceled.StoppedAt = &at2

	trash := todo("trash", "Trash")
	trash.Trashed = true

	template := todo("template", "Repeat")
	template.Recurrence = "template"
	template.StartDate = "2026-10-08"

	project := domain.Item{
		ID:     "someday-project",
		Kind:   "project",
		Title:  "Someday project",
		Status: "incomplete",
		Start:  "someday",
	}
	child := todo("child", "Child")
	child.ProjectID = project.ID

	s, m := testService(t,
		inbox, today, evening, overdue, future, someday,
		completed, canceled, trash, template, project, child,
	)

	cases := map[string][]string{
		"inbox":    {"inbox"},
		"today":    {"overdue", "today", "evening"},
		"evening":  {"evening"},
		"upcoming": {"future"},
		"someday":  {"child", "someday", "someday-project"},
		"logbook":  {"canceled", "completed"},
		"trash":    {"trash"},
	}
	for list, want := range cases {
		r, err := s.List(context.Background(), Query{List: list, All: true})
		if err != nil {
			t.Fatal(err)
		}

		items := r.Data.([]domain.Item)
		ids := []string{}
		for _, i := range items {
			ids = append(ids, i.ID)
		}
		if domain.Hash(ids) != domain.Hash(want) {
			t.Fatalf("%s: got %v want %v", list, ids, want)
		}
	}

	q := Query{List: "today", Limit: 1}
	first, err := s.List(context.Background(), q)
	if err != nil || first.Meta.NextCursor == nil {
		t.Fatal(first, err)
	}

	q.Cursor = *first.Meta.NextCursor
	next, err := s.List(context.Background(), q)
	if err != nil || next.Data.([]domain.Item)[0].ID == first.Data.([]domain.Item)[0].ID {
		t.Fatal(next, err)
	}

	m.mu.Lock()
	m.items[1].Title = "Changed"
	m.mu.Unlock()
	if _, err := s.List(context.Background(), q); domain.AsError(err).Code != "stale_cursor" {
		t.Fatal(err)
	}
}

func TestProjectDetailDoesNotDuplicateHeadingTasks(t *testing.T) {
	p := domain.Item{ID: "project", Kind: "project"}
	h := domain.Item{ID: "heading", Kind: "heading", ProjectID: p.ID}
	a := todo("a", "A")
	a.ProjectID = p.ID
	b := todo("b", "B")
	b.ProjectID = p.ID
	b.HeadingID = h.ID
	s, _ := testService(t, p, h, a, b)

	r, err := s.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}

	i := r.Data.(domain.Item)
	if len(i.Items) != 2 {
		t.Fatalf("top level duplicates: %+v", i.Items)
	}
	for _, c := range i.Items {
		if c.ID == b.ID {
			t.Fatal("heading child duplicated")
		}
		if c.ID == h.ID && (len(c.Items) != 1 || c.Items[0].ID != b.ID) {
			t.Fatal("heading child missing")
		}
	}
}

func TestDuplicateNamesNeverPickFirst(t *testing.T) {
	items := []domain.Item{
		{ID: "one", Kind: "project", Title: "Same"},
		{ID: "two", Kind: "project", Title: "Same"},
	}
	if _, err := resolve(items, "Same", "project", true); domain.AsError(err).Code != "item_ambiguous" {
		t.Fatal(err)
	}

	i, err := resolve(items, "things:///show?id=one", "", false)
	if err != nil || i.ID != "one" {
		t.Fatal(i, err)
	}
}

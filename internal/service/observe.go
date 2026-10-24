package service

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/readstore"
)

func (s *Service) observe(o *domain.Operation, snap readstore.Snapshot) bool {
	if o.Request.Action == "batch" {
		return false
	}

	create := o.Request.Action == "create_todo" || o.Request.Action == "create_project"
	candidates := []domain.Item{}
	if create {
		baseline := map[string]bool{}
		for _, id := range o.BaselineIDs {
			baseline[id] = true
		}

		for _, i := range snap.Items {
			if baseline[i.ID] || i.Trashed || i.ContextTrashed {
				continue
			}
			if (o.Request.Action == "create_todo" && i.Kind != "todo") || (o.Request.Action == "create_project" && i.Kind != "project") {
				continue
			}
			if len(o.ItemIDs) > 0 && !slices.Contains(o.ItemIDs, i.ID) {
				continue
			}
			if o.Request.Patch.Project == nil && o.Request.Patch.Area == nil && (i.ProjectID != "" || i.AreaID != "" || i.HeadingID != "") {
				continue
			}
			if matchPatch(i, o.Request.Patch, o.CreatedAt) && s.matchChildren(i, o.Request.Items, snap.Items, o.CreatedAt) {
				candidates = append(candidates, i)
			}
		}
	} else {
		for _, i := range snap.Items {
			if i.ID == o.Request.ID && !i.Trashed && !i.ContextTrashed && matchPatch(i, o.Request.Patch, o.CreatedAt) {
				candidates = append(candidates, i)
			}
		}
	}
	if len(candidates) != 1 {
		return false
	}

	o.ItemIDs = []string{candidates[0].ID}
	if create && len(o.Request.Items) > 0 {
		children := []string{}
		for _, i := range snap.Items {
			if i.ProjectID == candidates[0].ID && !i.Trashed && !i.ContextTrashed {
				children = append(children, i.ID)
			}
		}
		sort.Strings(children)
		o.ItemIDs = append(o.ItemIDs, children...)
	}

	o.State = "verified"
	o.Outcome = "confirmed"
	o.Errors = []*domain.Error{}
	o.Evidence = append(o.Evidence, domain.Evidence{
		Source:     "state_observation",
		ObservedAt: snap.ObservedAt,
		Detail:     "A fresh transaction matched the requested postcondition.",
	})
	return true
}

func matchPatch(i domain.Item, p domain.Patch, now time.Time) bool {
	// A to-do's public area includes its inherited project area. An explicit
	// area destination must also remove its project and heading assignment.
	if p.Area != nil && i.Kind == "todo" && (i.ProjectID != "" || i.HeadingID != "") {
		return false
	}

	for _, f := range []struct {
		want *string
		have string
	}{
		{p.Title, i.Title},
		{p.Notes, i.Notes},
		{p.Deadline, i.Deadline},
		{p.Reminder, i.Reminder},
		{p.Project, i.ProjectID},
		{p.Area, i.AreaID},
		{p.Heading, i.HeadingID},
		{p.Status, i.Status},
	} {
		if f.want != nil && *f.want != f.have {
			return false
		}
	}

	if p.When != nil {
		switch *p.When {
		case "":
			if i.StartDate != "" || i.Start != "anytime" {
				return false
			}
		case "anytime":
			if i.Start != "anytime" || i.StartDate != "" {
				return false
			}
		case "someday":
			if i.Start != "someday" || i.StartDate != "" {
				return false
			}
		case "evening":
			if !i.Evening || i.StartDate != now.In(time.Local).Format("2006-01-02") {
				return false
			}
		default:
			if i.StartDate != *p.When || i.Evening {
				return false
			}
		}
	}

	titles := []string{}
	for _, t := range i.Tags {
		titles = append(titles, t.ID)
	}
	sort.Strings(titles)
	if p.Tags != nil {
		want := append([]string{}, (*p.Tags)...)
		sort.Strings(want)
		if strings.Join(want, "\x00") != strings.Join(titles, "\x00") {
			return false
		}
	}
	for _, t := range p.AddTags {
		n := sort.SearchStrings(titles, t)
		if n == len(titles) || titles[n] != t {
			return false
		}
	}

	if p.Checklist != nil {
		if len(i.Checklist) != len(*p.Checklist) {
			return false
		}
		for n, c := range *p.Checklist {
			if c.Title != i.Checklist[n].Title || c.Status != i.Checklist[n].Status {
				return false
			}
		}
	}
	return true
}

func (s *Service) matchChildren(project domain.Item, requested []domain.ProjectChild, items []domain.Item, at time.Time) bool {
	if len(requested) == 0 {
		return true
	}

	actual := []domain.Item{}
	for _, i := range items {
		if i.ProjectID == project.ID && !i.Trashed && !i.ContextTrashed {
			actual = append(actual, i)
		}
	}
	if len(actual) != len(requested) {
		return false
	}

	// Headings and tasks have separate index spaces. Compare each heading's
	// ordered task group with the JSON input, including repeated titles.
	expectedHeadings := []domain.ProjectChild{}
	expectedTasks := [][]domain.ProjectChild{{}}
	for _, r := range requested {
		if r.Kind == "heading" {
			expectedHeadings = append(expectedHeadings, r)
			expectedTasks = append(expectedTasks, []domain.ProjectChild{})
		} else {
			n := len(expectedTasks) - 1
			expectedTasks[n] = append(expectedTasks[n], r)
		}
	}

	headings := []domain.Item{}
	tasks := map[string][]domain.Item{}
	for _, i := range actual {
		switch i.Kind {
		case "heading":
			headings = append(headings, i)
		case "todo":
			tasks[i.HeadingID] = append(tasks[i.HeadingID], i)
		default:
			return false
		}
	}
	if len(headings) != len(expectedHeadings) {
		return false
	}

	sortByIndex(headings)

	for n, expected := range expectedTasks {
		heading := ""
		if n > 0 {
			h := headings[n-1]
			if !matchPatch(h, expectedHeadings[n-1].Patch, at) {
				return false
			}
			heading = h.ID
		}

		group := tasks[heading]
		if len(group) != len(expected) {
			return false
		}
		sortByIndex(group)
		for j, r := range expected {
			if !matchPatch(group[j], r.Patch, at) {
				return false
			}
		}
	}
	return true
}

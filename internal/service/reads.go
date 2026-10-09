package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/urlscheme"
)

type Query struct {
	List    string `json:"list,omitempty"`
	Query   string `json:"query,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Project string `json:"project,omitempty"`
	Area    string `json:"area,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Status  string `json:"status,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	All     bool   `json:"all,omitempty"`
}

type cursor struct {
	Query    string `json:"q"`
	Snapshot string `json:"s"`
	Offset   int    `json:"o"`
}

func (s *Service) Open(ctx context.Context, id string) (domain.Response, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return r, err
	}
	i := r.Data.(domain.Item)

	dispatcher := s.Dispatcher
	if dispatcher == nil {
		dispatcher = urlscheme.System{}
	}
	if err := dispatcher.Dispatch(ctx, urlscheme.Show(i.ID)); err != nil {
		return domain.Response{}, err
	}

	r.Data = map[string]string{"item_id": i.ID, "outcome": "dispatched"}
	return r, nil
}

func resolve(items []domain.Item, selector, kind string, names bool) (domain.Item, error) {
	id, err := urlscheme.NormalizeID(selector)
	if err != nil {
		return domain.Item{}, err
	}
	if id == "" {
		return domain.Item{}, domain.E("invalid_selector", "An item selector is required.", 2)
	}

	matches := []domain.Item{}
	for _, i := range items {
		if kind != "" && i.Kind != kind {
			continue
		}
		if i.ID == id {
			return i, nil
		}
	}

	if names {
		for _, i := range items {
			if (kind == "" || i.Kind == kind) && i.Title == id {
				matches = append(matches, i)
			}
		}
	}
	if len(matches) == 0 {
		for _, i := range items {
			if (kind == "" || i.Kind == kind) && strings.HasPrefix(i.ID, id) {
				matches = append(matches, i)
			}
		}
	}

	if len(matches) == 0 {
		return domain.Item{}, domain.E("item_missing", "No matching item was found.", 3)
	}
	if len(matches) > 1 {
		return domain.Item{}, domain.E("item_ambiguous", "The selector matches multiple items. Use a full ID.", 3)
	}
	return matches[0], nil
}

func (s *Service) Get(ctx context.Context, id string) (domain.Response, error) {
	snap, err := s.Reader.Snapshot(ctx)
	if err != nil {
		return domain.Response{}, err
	}
	i, err := resolve(snap.Items, id, "", false)
	if err != nil {
		return domain.Response{}, err
	}

	if i.Kind == "project" || i.Kind == "heading" {
		children := []domain.Item{}
		for _, c := range snap.Items {
			if c.Trashed || c.ContextTrashed {
				continue
			}
			if (i.Kind == "project" && c.ProjectID == i.ID && (c.HeadingID == "" || c.Kind == "heading")) ||
				(i.Kind == "heading" && c.HeadingID == i.ID) {
				children = append(children, c)
			}
		}
		sortByIndex(children)

		for n := range children {
			if children[n].Kind == "heading" {
				nested := []domain.Item{}
				for _, c := range snap.Items {
					if c.HeadingID == children[n].ID && !c.Trashed && !c.ContextTrashed {
						nested = append(nested, c)
					}
				}
				sortByIndex(nested)
				children[n].Items = nested
			}
		}
		i.Items = children
	}

	return domain.Wrap(i, "sqlite", snap.ObservedAt), nil
}

func (s *Service) List(ctx context.Context, q Query) (domain.Response, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 500 {
		return domain.Response{}, domain.E("invalid_limit", "Limit must be between 1 and 500. Use --all for an export.", 2)
	}
	if q.All && q.Cursor != "" {
		return domain.Response{}, domain.E("invalid_cursor", "A cursor cannot be combined with all.", 2)
	}

	validLists := map[string]bool{
		"":         true,
		"inbox":    true,
		"today":    true,
		"evening":  true,
		"upcoming": true,
		"anytime":  true,
		"someday":  true,
		"logbook":  true,
		"trash":    true,
	}
	if !validLists[q.List] {
		return domain.Response{}, domain.E("invalid_list", "Unknown Things list.", 2)
	}
	if q.Kind != "" && q.Kind != "todo" && q.Kind != "project" && q.Kind != "area" && q.Kind != "tag" && q.Kind != "heading" {
		return domain.Response{}, domain.E("invalid_kind", "Unknown item kind.", 2)
	}
	if q.Status != "" && q.Status != "incomplete" && q.Status != "completed" && q.Status != "canceled" && q.Status != "all" {
		return domain.Response{}, domain.E("invalid_status", "Status must be incomplete, completed, canceled, or all.", 2)
	}

	snap, err := s.Reader.Snapshot(ctx)
	if err != nil {
		return domain.Response{}, err
	}

	for _, f := range []struct {
		value *string
		kind  string
	}{
		{&q.Project, "project"},
		{&q.Area, "area"},
		{&q.Tag, "tag"},
	} {
		if *f.value != "" {
			i, err := resolve(snap.Items, *f.value, f.kind, true)
			if err != nil {
				return domain.Response{}, err
			}
			*f.value = i.ID
		}
	}

	byID := map[string]domain.Item{}
	for _, i := range snap.Items {
		byID[i.ID] = i
	}

	today := s.now().In(time.Local).Format("2006-01-02")
	result := []domain.Item{}
	for _, i := range snap.Items {
		if q.Kind != "" {
			if i.Kind != q.Kind {
				continue
			}
		} else if i.Kind != "todo" && i.Kind != "project" {
			continue
		}
		if q.List == "trash" {
			if !i.Trashed && !i.ContextTrashed {
				continue
			}
		} else if i.Trashed || i.ContextTrashed {
			continue
		}

		if q.Project != "" && i.ProjectID != q.Project {
			continue
		}
		if q.Area != "" && i.AreaID != q.Area {
			continue
		}
		if q.Tag != "" {
			found := false
			for _, t := range i.Tags {
				if t.ID == q.Tag {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if q.Query != "" && !strings.Contains(strings.ToLower(i.Title+"\n"+i.Notes), strings.ToLower(q.Query)) {
			continue
		}

		status := q.Status
		if status == "" {
			if q.List == "logbook" || q.List == "trash" || q.Kind == "area" || q.Kind == "tag" {
				status = "all"
			} else {
				status = "incomplete"
			}
		}
		if status != "all" && i.Status != status {
			continue
		}
		if i.Recurrence == "template" && q.List != "" {
			continue
		}

		p := byID[i.ProjectID]
		parentSomeday := p.ID != "" && p.Start == "someday"
		match := true
		switch q.List {
		case "inbox":
			match = i.Start == "inbox" && i.Kind == "todo"
		case "today", "evening":
			match = (i.Start == "anytime" && i.StartDate != "") ||
				(i.Start == "someday" && i.StartDate != "" && i.StartDate <= today) ||
				(i.StartDate == "" && i.Deadline != "" && i.Deadline <= today && !i.DeadlineSuppressed)
			if q.List == "evening" {
				match = match && i.Evening
			}
		case "upcoming":
			match = i.Start == "someday" && i.StartDate > today
		case "anytime":
			match = i.Start == "anytime" && !parentSomeday
		case "someday":
			match = (i.Start == "someday" && i.StartDate == "") || (parentSomeday && p.StartDate == "" && i.StartDate == "")
		case "logbook":
			match = i.Status == "completed" || i.Status == "canceled"
		}
		if match {
			result = append(result, i)
		}
	}

	ordering := "index,id"
	if q.List == "today" || q.List == "evening" {
		ordering = "evening,today_index,start_date,id"
	}
	if q.List == "upcoming" {
		ordering = "start_date,index,id"
	}
	if q.List == "logbook" {
		ordering = "stopped_at_desc,index,id"
	}
	sort.SliceStable(result, func(a, b int) bool {
		x, y := result[a], result[b]
		if q.List == "today" || q.List == "evening" {
			if x.Evening != y.Evening {
				return !x.Evening
			}
			if x.TodayIndex != y.TodayIndex {
				return x.TodayIndex < y.TodayIndex
			}
			if x.StartDate != y.StartDate {
				return x.StartDate < y.StartDate
			}
		}
		if q.List == "upcoming" && x.StartDate != y.StartDate {
			return x.StartDate < y.StartDate
		}
		if q.List == "logbook" {
			xt, yt := time.Time{}, time.Time{}
			if x.StoppedAt != nil {
				xt = *x.StoppedAt
			}
			if y.StoppedAt != nil {
				yt = *y.StoppedAt
			}
			if !xt.Equal(yt) {
				return xt.After(yt)
			}
		}

		if x.Index != y.Index {
			return x.Index < y.Index
		}
		return x.ID < y.ID
	})

	offset := 0
	rawCursor := q.Cursor
	q.Cursor = ""
	queryHash := domain.Hash(struct {
		Query Query
		Date  string
	}{q, today})
	snapshotHash := domain.Hash(result)
	if rawCursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(rawCursor)
		if err != nil {
			return domain.Response{}, domain.E("invalid_cursor", "Invalid pagination cursor.", 2)
		}
		var c cursor
		if err := json.Unmarshal(b, &c); err != nil || c.Query != queryHash || c.Snapshot != snapshotHash || c.Offset < 0 || c.Offset >= len(result) {
			return domain.Response{}, domain.E("stale_cursor", "The cursor does not match this query or the library changed. Start a new query.", 2)
		}
		offset = c.Offset
	}

	end := len(result)
	var next *string
	if !q.All && end > offset+q.Limit {
		end = offset + q.Limit
		b, _ := json.Marshal(cursor{queryHash, snapshotHash, end})
		v := base64.RawURLEncoding.EncodeToString(b)
		next = &v
	}

	response := domain.Wrap(result[offset:end], "sqlite", snap.ObservedAt)
	response.Meta.NextCursor = next
	response.Meta.Ordering = ordering
	return response, nil
}

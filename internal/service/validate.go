package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/readstore"
)

func (s *Service) prepare(ctx context.Context, r domain.Request) (domain.Request, readstore.Snapshot, *domain.Item, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return r, readstore.Snapshot{}, nil, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, readstore.Snapshot{}, nil, err
	}

	if r.Action != "create_todo" && r.Action != "create_project" && r.Action != "update" && r.Action != "update_project" && r.Action != "set_status" {
		return r, readstore.Snapshot{}, nil, domain.E("unsupported_action", "This action is unsupported.", 5)
	}
	if len(r.OperationKey) > 256 {
		return r, readstore.Snapshot{}, nil, domain.E("invalid_operation_key", "Operation keys must be at most 256 bytes.", 2)
	}

	snap, err := s.Reader.Snapshot(ctx)
	if err != nil {
		return r, snap, nil, err
	}

	if r.Action == "set_status" {
		if r.Patch.Status == nil || domain.Hash(r.Patch) != domain.Hash(domain.Patch{Status: r.Patch.Status}) {
			return r, snap, nil, domain.E("invalid_status_patch", "set_status accepts only a status field.", 2)
		}
		i, err := resolve(snap.Items, r.ID, "", false)
		if err != nil {
			return r, snap, nil, err
		}
		r.Action = "update"
		if i.Kind == "project" {
			r.Action = "update_project"
		}
	}

	var before *domain.Item
	if r.Action == "update" || r.Action == "update_project" {
		kind := "todo"
		if r.Action == "update_project" {
			kind = "project"
		}

		i, err := resolve(snap.Items, r.ID, "", false)
		if err != nil {
			return r, snap, nil, err
		}
		if i.Kind != kind {
			return r, snap, nil, domain.E("wrong_item_kind", "This command cannot update the selected item kind.", 5)
		}
		if i.Trashed || i.ContextTrashed {
			return r, snap, nil, domain.E("trashed_item", "Restore this item in Things before editing it.", 5)
		}
		if i.Recurrence == "template" && (r.Patch.When != nil || r.Patch.Deadline != nil || r.Patch.Status != nil || r.Patch.Reminder != nil) {
			return r, snap, nil, domain.E("recurrence_edit_unsupported", "The URL scheme cannot change these fields on recurring templates.", 5)
		}
		if r.ExpectedFingerprint != "" && r.ExpectedFingerprint != i.Fingerprint {
			return r, snap, nil, domain.E("fingerprint_conflict", "The item changed since the expected snapshot.", 6)
		}
		if len(r.Items) > 0 {
			return r, snap, nil, domain.E("heading_edit_unsupported", "Nested structures can only be supplied when creating a project.", 5)
		}

		r.ID = i.ID
		before = &i
	} else {
		if r.ID != "" || r.ExpectedFingerprint != "" {
			return r, snap, nil, domain.E("invalid_create", "Creation cannot specify an existing ID or fingerprint.", 2)
		}
		if r.Patch.Title == nil || strings.TrimSpace(*r.Patch.Title) == "" {
			return r, snap, nil, domain.E("title_required", "Creation requires a nonempty title.", 2)
		}
		if r.Action == "create_todo" && len(r.Items) > 0 {
			return r, snap, nil, domain.E("invalid_children", "Only new projects can have nested items.", 2)
		}

		if r.Patch.Notes == nil {
			r.Patch.Notes = str("")
		}
		if r.Patch.Status == nil {
			r.Patch.Status = str("incomplete")
		}
		if r.Patch.Tags == nil {
			v := []string{}
			r.Patch.Tags = &v
		}
	}

	if err := s.validatePatch(&r.Patch, r.Action, before, snap.Items); err != nil {
		return r, snap, before, err
	}
	if len(r.Items) > 249 {
		return r, snap, before, domain.E("item_limit", "A project can contain at most 249 new items per request.", 2)
	}

	for n := range r.Items {
		c := &r.Items[n]
		if c.Kind == "heading" {
			if c.Patch.Title == nil || strings.TrimSpace(*c.Patch.Title) == "" {
				return r, snap, before, domain.E("title_required", "Headings require a title.", 2)
			}
			if domain.Hash(c.Patch) != domain.Hash(domain.Patch{Title: c.Patch.Title}) {
				return r, snap, before, domain.E("heading_fields_unsupported", "New headings support only a title.", 5)
			}
			if err := validateText(*c.Patch.Title, 4000, "title"); err != nil {
				return r, snap, before, err
			}
			continue
		}

		if c.Kind != "todo" {
			return r, snap, before, domain.E("invalid_child_kind", "Project items must be todo or heading.", 2)
		}
		if c.Patch.Title == nil || strings.TrimSpace(*c.Patch.Title) == "" {
			return r, snap, before, domain.E("title_required", "Nested tasks require a title.", 2)
		}
		if c.Patch.Project != nil || c.Patch.Area != nil || c.Patch.Heading != nil {
			return r, snap, before, domain.E("invalid_child_destination", "Nested tasks belong to the new project and preceding heading. Omit destination fields.", 2)
		}
		if err := s.validatePatch(&c.Patch, "create_todo", nil, snap.Items); err != nil {
			return r, snap, before, err
		}
	}

	if before != nil && before.Kind == "project" && r.Patch.Status != nil && *r.Patch.Status != "incomplete" {
		for _, i := range snap.Items {
			if i.ProjectID == before.ID && !i.Trashed && i.Status == "incomplete" {
				return r, snap, before, domain.E("project_not_ready", "Complete or cancel child tasks and archive headings in Things before closing this project.", 6)
			}
		}
	}
	if r.Action == "create_project" && r.Patch.Status != nil && *r.Patch.Status != "incomplete" {
		for _, c := range r.Items {
			if c.Kind == "heading" || c.Patch.Status == nil || *c.Patch.Status == "incomplete" {
				return r, snap, before, domain.E("project_not_ready", "A closed new project cannot contain incomplete tasks or active headings.", 6)
			}
		}
	}
	return r, snap, before, nil
}

func validateText(v string, max int, field string) error {
	if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
		return domain.E("invalid_text", field+" must contain valid UTF-8 without NUL bytes.", 2)
	}
	if utf8.RuneCountInString(v) > max {
		return domain.E("field_limit", field+" exceeds the documented character limit.", 2)
	}
	return nil
}

func (s *Service) validatePatch(p *domain.Patch, action string, before *domain.Item, items []domain.Item) error {
	for _, f := range []struct {
		name string
		v    *string
		max  int
	}{
		{"title", p.Title, 4000},
		{"notes", p.Notes, 10000},
	} {
		if f.v != nil {
			if err := validateText(*f.v, f.max, f.name); err != nil {
				return err
			}
		}
	}

	if p.Status != nil && *p.Status != "incomplete" && *p.Status != "completed" && *p.Status != "canceled" {
		return domain.E("invalid_status", "Status must be incomplete, completed, or canceled.", 2)
	}
	if p.Project != nil && p.Area != nil {
		return domain.E("conflicting_destination", "Choose either a project or an area.", 2)
	}
	if (action == "create_project" || action == "update_project") && (p.Project != nil || p.Heading != nil || p.Checklist != nil) {
		return domain.E("project_field_unsupported", "Projects do not support project, heading, or checklist fields.", 5)
	}

	for _, f := range []struct {
		v    *string
		kind string
	}{
		{p.Project, "project"},
		{p.Area, "area"},
		{p.Heading, "heading"},
	} {
		if f.v == nil {
			continue
		}
		if *f.v == "" {
			return domain.E("destination_clear_unsupported", "Clearing a destination is unsupported by this adapter.", 5)
		}

		i, err := resolve(items, *f.v, f.kind, true)
		if err != nil {
			return err
		}
		if i.Trashed || i.ContextTrashed || i.Status == "completed" || i.Status == "canceled" {
			return domain.E("invalid_destination", "The destination is trashed or closed.", 3)
		}
		*f.v = i.ID
	}

	if p.Heading != nil {
		project := ""
		if p.Project != nil {
			project = *p.Project
		} else if before != nil {
			project = before.ProjectID
		}

		h, err := resolve(items, *p.Heading, "heading", false)
		if err != nil {
			return err
		}
		if project == "" || h.ProjectID != project || p.Area != nil {
			return domain.E("heading_project_mismatch", "The heading must belong to the selected project.", 2)
		}
	}

	if p.Tags != nil && len(p.AddTags) > 0 {
		return domain.E("conflicting_tags", "Tag replacement and tag addition cannot be combined.", 2)
	}
	if (action == "create_todo" || action == "create_project") && len(p.AddTags) > 0 {
		return domain.E("add_tags_unsupported", "Creation supports tag replacement, not add_tags.", 5)
	}

	if p.Tags != nil {
		v, err := resolveTags(items, *p.Tags)
		if err != nil {
			return err
		}
		p.Tags = &v
	}
	if len(p.AddTags) > 0 {
		v, err := resolveTags(items, p.AddTags)
		if err != nil {
			return err
		}
		p.AddTags = v
	}

	if p.Checklist != nil {
		if err := validateChecklist(*p.Checklist, action); err != nil {
			return err
		}
	}

	today := s.now().In(time.Local)
	if p.When != nil {
		v, err := normalizeDate(*p.When, today, true)
		if err != nil {
			return err
		}
		if (action == "update" || action == "update_project") && v == "anytime" {
			return domain.E("when_unsupported", "The documented update command does not support anytime. Clear when instead.", 5)
		}
		p.When = &v
	}
	if p.Deadline != nil {
		v, err := normalizeDate(*p.Deadline, today, false)
		if err != nil {
			return err
		}
		p.Deadline = &v
	}

	if p.Reminder != nil && p.When == nil {
		if before == nil || before.StartDate == "" {
			return domain.E("reminder_schedule_required", "A reminder requires a scheduled day or evening.", 2)
		}
		v := before.StartDate
		if before.Evening {
			if before.StartDate != today.Format("2006-01-02") {
				return domain.E("evening_reminder_unsupported", "The URL scheme cannot preserve a non-current Evening date during a reminder-only change. Change the schedule explicitly or edit the reminder in Things.", 5)
			}
			v = "evening"
		}
		p.When = &v
	}
	if p.Reminder == nil && p.When != nil && before != nil {
		v := before.Reminder
		if *p.When == "" || *p.When == "someday" || *p.When == "anytime" {
			v = ""
		}
		p.Reminder = &v
	}

	if p.Reminder != nil {
		if *p.Reminder != "" {
			if _, err := time.Parse("15:04", *p.Reminder); err != nil || len(*p.Reminder) != 5 {
				return domain.E("invalid_reminder", "Use HH:MM in local time for a reminder.", 2)
			}
			if p.When == nil || *p.When == "" || *p.When == "anytime" || *p.When == "someday" {
				return domain.E("reminder_schedule_required", "A reminder requires a scheduled day or evening.", 2)
			}
		}
	}

	if before != nil && domain.Hash(*p) == domain.Hash(domain.Patch{}) {
		return domain.E("empty_patch", "Specify at least one supported change.", 2)
	}
	return nil
}

func resolveTags(items []domain.Item, tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		i, err := resolve(items, tag, "tag", true)
		if err != nil {
			return nil, err
		}
		for _, other := range items {
			if other.Kind == "tag" && other.ID != i.ID && other.Title == i.Title {
				return nil, domain.E("ambiguous_tag_title", "The URL scheme requires a unique tag title, even when selected by ID.", 3)
			}
		}
		if strings.ContainsAny(i.Title, ",\n\r") {
			return nil, domain.E("tag_encoding_unsupported", "This tag title cannot be represented by the Things URL scheme.", 5)
		}
		if !seen[i.ID] {
			out = append(out, i.ID)
			seen[i.ID] = true
		}
	}

	sort.Strings(out)
	if err := validateText(strings.Join(tagTitles(items, out), ","), 4000, "tags"); err != nil {
		return nil, err
	}
	return out, nil
}

func validateChecklist(rows []domain.ChecklistItem, action string) error {
	if len(rows) > 100 {
		return domain.E("checklist_limit", "A checklist can contain at most 100 rows.", 2)
	}

	total := []string{}
	for n := range rows {
		c := &rows[n]
		if c.ID != "" {
			return domain.E("checklist_id_unsupported", "Checklist replacement cannot target existing checklist IDs.", 5)
		}
		if c.Status == "" {
			c.Status = "incomplete"
		}
		if c.Status != "incomplete" && c.Status != "completed" && c.Status != "canceled" {
			return domain.E("invalid_status", "Invalid checklist status.", 2)
		}

		if strings.ContainsAny(c.Title, "\n\r") {
			return domain.E("invalid_checklist", "Checklist titles cannot contain newlines.", 2)
		}
		if err := validateText(c.Title, 4000, "checklist title"); err != nil {
			return err
		}
		total = append(total, c.Title)

		if (action == "update" || action == "update_project") && c.Status != "incomplete" {
			return domain.E("checklist_status_unsupported", "Checklist replacement through the URL scheme creates incomplete rows.", 5)
		}
	}
	return validateText(strings.Join(total, "\n"), 4000, "checklist")
}

// normalizeDate resolves today and tomorrow and validates ISO dates.
// When keywords is true, it also accepts evening, anytime, and someday.
func normalizeDate(v string, today time.Time, keywords bool) (string, error) {
	switch v {
	case "today":
		return today.Format("2006-01-02"), nil
	case "tomorrow":
		return today.AddDate(0, 0, 1).Format("2006-01-02"), nil
	case "evening", "anytime", "someday":
		if keywords {
			return v, nil
		}
	}

	if v == "" {
		return v, nil
	}
	if _, err := time.ParseInLocation("2006-01-02", v, time.Local); err != nil {
		return "", domain.E("invalid_date", "Use an ISO calendar date or a supported Things scheduling value.", 2)
	}
	return v, nil
}

// tagTitles converts verified tag identities only at the URL encoding boundary.
func tagTitles(items []domain.Item, ids []string) []string {
	titles := []string{}
	for _, id := range ids {
		for _, i := range items {
			if i.Kind == "tag" && i.ID == id {
				titles = append(titles, i.Title)
			}
		}
	}
	sort.Strings(titles)
	return titles
}

func dispatchRequest(r domain.Request, items []domain.Item) domain.Request {
	convert := func(p *domain.Patch) {
		if p.Tags != nil {
			titles := tagTitles(items, *p.Tags)
			p.Tags = &titles
		}
		p.AddTags = tagTitles(items, p.AddTags)
	}
	convert(&r.Patch)
	r.Items = append([]domain.ProjectChild{}, r.Items...)
	for n := range r.Items {
		convert(&r.Items[n].Patch)
	}
	return r
}

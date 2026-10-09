package cmd

import (
	"io"
	"os"
	"strings"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/spf13/cobra"
)

type mutationFlags struct {
	title, notes, notesFile, when, deadline, reminder, project, area, heading, file, key, fingerprint string
	tags, addTags, checklist                                                                          []string
	clearNotes, clearWhen, clearDeadline, clearReminder, clearTags, clearChecklist, dryRun            bool
}

func bindMutation(c *cobra.Command, f *mutationFlags, update, project bool) {
	if update {
		c.Flags().StringVar(&f.title, "title", "", "Replace the title")
	}
	c.Flags().StringVar(&f.notes, "notes", "", "Set notes")
	c.Flags().StringVar(&f.notesFile, "notes-file", "", "Read multiline notes from a UTF-8 file")
	c.Flags().StringVar(&f.when, "when", "", "Schedule with a date, today, tomorrow, evening, anytime, or someday")
	c.Flags().StringVar(&f.deadline, "deadline", "", "Set an ISO calendar deadline")
	c.Flags().StringVar(&f.reminder, "reminder", "", "Set a local HH:MM reminder on a scheduled day")
	c.Flags().StringVar(&f.area, "area", "", "Assign an area ID or unique name")
	if !project {
		c.Flags().StringVar(&f.project, "project", "", "Assign a project ID or unique name")
		c.Flags().StringVar(&f.heading, "heading", "", "Assign a heading in the selected project")
		c.Flags().StringArrayVar(&f.checklist, "checklist-item", nil, "Set checklist rows; repeat for each row")
	}
	c.Flags().StringArrayVar(&f.tags, "tag", nil, "Replace tags; repeat for each existing tag")

	if update {
		c.Flags().StringArrayVar(&f.addTags, "add-tag", nil, "Add existing tags without replacing others")
		c.Flags().BoolVar(&f.clearNotes, "clear-notes", false, "Clear notes")
		c.Flags().BoolVar(&f.clearWhen, "clear-when", false, "Clear scheduling")
		c.Flags().BoolVar(&f.clearDeadline, "clear-deadline", false, "Clear the deadline")
		c.Flags().BoolVar(&f.clearReminder, "clear-reminder", false, "Clear a reminder")
		c.Flags().BoolVar(&f.clearTags, "clear-tags", false, "Clear tags")
		if !project {
			c.Flags().BoolVar(&f.clearChecklist, "clear-checklist", false, "Clear checklist rows")
		}
	}

	c.Flags().StringVar(&f.file, "file", "", "Read a typed request from JSON")
	c.Flags().StringVar(&f.key, "operation-key", "", "Protect this request against replay")
	c.Flags().StringVar(&f.fingerprint, "expected-fingerprint", "", "Reject a changed item snapshot")
	c.Flags().BoolVar(&f.dryRun, "dry-run", false, "Validate and print changes without dispatch")
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, domain.E("input_file_access", "Cannot read the input file.", 2)
	}
	defer f.Close()

	b, err := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
	if err != nil {
		return nil, domain.E("input_file_access", "Cannot read the input file.", 2)
	}
	if len(b) > 2*1024*1024 {
		return nil, domain.E("input_file_limit", "Input files must be at most 2 MiB.", 2)
	}
	return b, nil
}

func requestFromFlags(c *cobra.Command, f mutationFlags, action, id, title string) (domain.Request, error) {
	r := domain.Request{
		Action:              action,
		ID:                  id,
		OperationKey:        f.key,
		ExpectedFingerprint: f.fingerprint,
		DryRun:              f.dryRun,
	}

	if f.file != "" {
		b, err := readFile(f.file)
		if err != nil {
			return r, err
		}

		var input struct {
			Patch domain.Patch          `json:"patch"`
			Items []domain.ProjectChild `json:"items,omitempty"`
		}
		if err := domain.StrictDecode(b, &input); err != nil {
			return r, err
		}
		r.Patch = input.Patch
		r.Items = input.Items
	}

	if title != "" {
		r.Patch.Title = &title
	}

	for _, v := range []struct {
		flag   string
		value  string
		target **string
	}{
		{"title", f.title, &r.Patch.Title},
		{"notes", f.notes, &r.Patch.Notes},
		{"when", f.when, &r.Patch.When},
		{"deadline", f.deadline, &r.Patch.Deadline},
		{"reminder", f.reminder, &r.Patch.Reminder},
		{"project", f.project, &r.Patch.Project},
		{"area", f.area, &r.Patch.Area},
		{"heading", f.heading, &r.Patch.Heading},
	} {
		if c.Flags().Changed(v.flag) {
			if *v.target != nil {
				return r, domain.E("duplicate_field", "A field cannot be supplied in both JSON and flags: "+v.flag, 2)
			}
			value := v.value
			*v.target = &value
		}
	}

	if f.notesFile != "" {
		if r.Patch.Notes != nil {
			return r, domain.E("conflicting_notes", "Choose one notes input.", 2)
		}
		b, err := readFile(f.notesFile)
		if err != nil {
			return r, err
		}
		v := string(b)
		r.Patch.Notes = &v
	}

	for _, v := range []struct {
		clear  bool
		target **string
		name   string
	}{
		{f.clearNotes, &r.Patch.Notes, "notes"},
		{f.clearWhen, &r.Patch.When, "when"},
		{f.clearDeadline, &r.Patch.Deadline, "deadline"},
		{f.clearReminder, &r.Patch.Reminder, "reminder"},
	} {
		if v.clear {
			if *v.target != nil {
				return r, domain.E("conflicting_clear", "A field cannot be set and cleared: "+v.name, 2)
			}
			empty := ""
			*v.target = &empty
		}
	}

	if c.Flags().Changed("tag") || f.clearTags {
		if r.Patch.Tags != nil || f.clearTags && len(f.tags) > 0 {
			return r, domain.E("conflicting_tags", "Choose one tag replacement input.", 2)
		}
		tags := f.tags
		if tags == nil {
			tags = []string{}
		}
		r.Patch.Tags = &tags
	}
	if len(f.addTags) > 0 {
		if len(r.Patch.AddTags) > 0 {
			return r, domain.E("conflicting_tags", "Choose one tag addition input.", 2)
		}
		r.Patch.AddTags = f.addTags
	}

	if len(f.checklist) > 0 || f.clearChecklist {
		if r.Patch.Checklist != nil || f.clearChecklist && len(f.checklist) > 0 {
			return r, domain.E("conflicting_checklist", "Choose one checklist input.", 2)
		}
		rows := []domain.ChecklistItem{}
		for _, title := range f.checklist {
			rows = append(rows, domain.ChecklistItem{Title: title, Status: "incomplete"})
		}
		r.Patch.Checklist = &rows
	}

	return r, nil
}

func (a *app) addMutations(root *cobra.Command) {
	add := func(parent *cobra.Command, use, action string, update, project bool) {
		var f mutationFlags
		c := &cobra.Command{
			Use: use,
			Short: map[string]string{
				"create_todo":    "Create a to-do and verify its saved fields",
				"create_project": "Create a project and verify its saved fields",
				"update":         "Patch a to-do and verify its saved fields",
				"update_project": "Patch a project and verify its saved fields",
			}[action],
			Args: cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				id, title := "", ""
				if update {
					id = args[0]
				} else {
					title = args[0]
				}

				r, err := requestFromFlags(c, f, action, id, title)
				if err != nil {
					return err
				}

				s, err := a.getService(true)
				if err != nil {
					return err
				}
				o, err := s.Mutate(c.Context(), r)
				return a.mutation(c, o, err)
			},
		}
		bindMutation(c, &f, update, project)
		parent.AddCommand(c)
	}

	add(root, "add TITLE", "create_todo", false, false)
	add(root, "update ID", "update", true, false)

	project := &cobra.Command{
		Use:   "project",
		Short: "Create or update a project",
	}
	add(project, "add TITLE", "create_project", false, true)
	add(project, "update ID", "update_project", true, true)
	root.AddCommand(project)

	for _, verb := range []string{"complete", "cancel", "reopen"} {
		var f mutationFlags
		status := map[string]string{
			"complete": "completed",
			"cancel":   "canceled",
			"reopen":   "incomplete",
		}[verb]
		c := &cobra.Command{
			Use:   verb + " ID",
			Short: strings.ToUpper(verb[:1]) + verb[1:] + " a to-do or project",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				s, err := a.getService(true)
				if err != nil {
					return err
				}

				o, err := s.Mutate(c.Context(), domain.Request{
					Action:              "set_status",
					ID:                  args[0],
					Patch:               domain.Patch{Status: &status},
					OperationKey:        f.key,
					ExpectedFingerprint: f.fingerprint,
					DryRun:              f.dryRun,
				})
				return a.mutation(c, o, err)
			},
		}
		c.Flags().StringVar(&f.key, "operation-key", "", "Protect this request against replay")
		c.Flags().StringVar(&f.fingerprint, "expected-fingerprint", "", "Reject a changed snapshot")
		c.Flags().BoolVar(&f.dryRun, "dry-run", false, "Validate without dispatch")
		root.AddCommand(c)
	}

	var file, key string
	var dryRun bool
	batch := &cobra.Command{
		Use:   "batch --file PLAN.json",
		Short: "Validate all actions and execute a recoverable batch",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if file == "" {
				return domain.E("file_required", "A batch JSON file is required.", 2)
			}

			b, err := readFile(file)
			if err != nil {
				return err
			}

			var plan domain.BatchRequest
			if err := domain.StrictDecode(b, &plan); err != nil {
				return err
			}
			if key != "" {
				if plan.OperationKey != "" && plan.OperationKey != key {
					return domain.E("conflicting_operation_key", "Choose one operation key.", 2)
				}
				plan.OperationKey = key
			}
			plan.DryRun = plan.DryRun || dryRun

			s, err := a.getService(true)
			if err != nil {
				return err
			}
			o, err := s.Batch(c.Context(), plan)
			return a.mutation(c, o, err)
		},
	}
	batch.Flags().StringVar(&file, "file", "", "Read a typed batch JSON plan")
	batch.Flags().StringVar(&key, "operation-key", "", "Protect the entire batch against replay")
	batch.Flags().BoolVar(&dryRun, "dry-run", false, "Validate all actions without dispatch")
	root.AddCommand(batch)
}

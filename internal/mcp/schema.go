package mcp

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mattt/things-cli/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func schemas[In any](t *sdk.Tool) {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(err)
	}
	annotate(s)

	if p := s.Properties["patch"]; p != nil {
		switch t.Name {
		case "create_todo":
			delete(p.Properties, "add_tags")
			p.Required = append(p.Required, "title")
		case "create_project":
			for _, field := range []string{"project", "heading", "checklist", "add_tags"} {
				delete(p.Properties, field)
			}
			p.Required = append(p.Required, "title")
		}
	}
	if t.Name == "create_project" {
		if items := s.Properties["items"]; items != nil && items.Items != nil {
			if p := items.Items.Properties["patch"]; p != nil {
				for _, field := range []string{"project", "area", "heading", "add_tags"} {
					delete(p.Properties, field)
				}
				p.Required = append(p.Required, "title")
			}
		}
	}
	if t.Name == "list_containers" {
		s.Properties["kind"].Enum = []any{"project", "area", "tag", "heading"}
	}
	if t.Name == "update_item" {
		s.Properties["kind"].Enum = []any{"todo", "project"}
	}
	t.InputSchema = s

	output, err := jsonschema.For[domain.Response](nil)
	if err != nil {
		panic(err)
	}
	var version any = domain.SchemaVersion
	output.Properties["schema_version"].Const = &version
	t.OutputSchema = output
}

func annotate(s *jsonschema.Schema) {
	if s == nil {
		return
	}

	for name, p := range s.Properties {
		switch name {
		case "list":
			p.Enum = []any{"inbox", "today", "evening", "upcoming", "anytime", "someday", "logbook", "trash"}
		case "action":
			p.Enum = []any{"create_todo", "create_project", "update", "update_project", "set_status"}
		case "when":
			p.Description = "ISO local date, today, tomorrow, evening, anytime, or someday where supported. Null clears scheduling."
		case "deadline":
			p.Description = "ISO local calendar date, today, or tomorrow; null clears the deadline."
		case "reminder":
			p.Description = "HH:MM in local time, with a compatible scheduled day or evening. Null clears it."
		case "tags":
			p.Description = "Replace existing tags by uniquely resolved name or ID. Null clears tags."
		case "add_tags":
			p.Description = "Add existing tags without replacing unrelated tags."
		case "operation_key":
			p.Description = "Local replay protection for the same request. Uncertain requests are never resent."
		case "checklist":
			if p.Items != nil {
				delete(p.Items.Properties, "id")
			}
		}
		annotate(p)
	}
	annotate(s.Items)
	for _, def := range s.Defs {
		annotate(def)
	}
}

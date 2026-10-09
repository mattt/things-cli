package domain

import (
	"bytes"
	"encoding/json"
	"io"
)

// Pointer fields distinguish omitted values from explicit empty values.
// JSON null is normalized to an empty value by UnmarshalJSON.
type Patch struct {
	Title     *string          `json:"title,omitempty"`
	Notes     *string          `json:"notes,omitempty"`
	When      *string          `json:"when,omitempty"`
	Deadline  *string          `json:"deadline,omitempty"`
	Reminder  *string          `json:"reminder,omitempty"`
	Project   *string          `json:"project,omitempty"`
	Area      *string          `json:"area,omitempty"`
	Heading   *string          `json:"heading,omitempty"`
	Tags      *[]string        `json:"tags,omitempty"`
	AddTags   []string         `json:"add_tags,omitempty"`
	Checklist *[]ChecklistItem `json:"checklist,omitempty"`
	Status    *string          `json:"status,omitempty"`
}

func (p *Patch) UnmarshalJSON(b []byte) error {
	type alias Patch
	var a alias
	if err := StrictDecode(b, &a); err != nil {
		return err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}

	for key, v := range fields {
		if string(v) != "null" {
			continue
		}
		empty := ""
		switch key {
		case "title":
			a.Title = &empty
		case "notes":
			a.Notes = &empty
		case "when":
			a.When = &empty
		case "deadline":
			a.Deadline = &empty
		case "reminder":
			a.Reminder = &empty
		case "project":
			a.Project = &empty
		case "area":
			a.Area = &empty
		case "heading":
			a.Heading = &empty
		case "status":
			a.Status = &empty
		case "tags":
			v := []string{}
			a.Tags = &v
		case "checklist":
			v := []ChecklistItem{}
			a.Checklist = &v
		}
	}

	*p = Patch(a)
	return nil
}

type ProjectChild struct {
	Kind  string `json:"kind"`
	Patch Patch  `json:"patch"`
}

type Request struct {
	Action              string         `json:"action"`
	ID                  string         `json:"id,omitempty"`
	Patch               Patch          `json:"patch"`
	Items               []ProjectChild `json:"items,omitempty"`
	OperationKey        string         `json:"operation_key,omitempty"`
	ExpectedFingerprint string         `json:"expected_fingerprint,omitempty"`
	DryRun              bool           `json:"dry_run,omitempty"`
}

type BatchRequest struct {
	Actions      []Request `json:"actions"`
	OperationKey string    `json:"operation_key,omitempty"`
	DryRun       bool      `json:"dry_run,omitempty"`
}

func StrictDecode(b []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return E("invalid_json", "Invalid structured input: "+err.Error(), 2)
	}

	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return E("invalid_json", "Structured input must contain one JSON value.", 2)
	}
	return nil
}

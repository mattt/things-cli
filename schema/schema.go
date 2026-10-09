// Package schema generates the public batch schema from the same types used by CLI and MCP.
package schema

//go:generate go run generate.go

import (
	"bytes"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mattt/things-cli/internal/domain"
)

// Batch returns the JSON Schema for a batch plan, as published in batch.schema.json.
func Batch() ([]byte, error) {
	s, err := jsonschema.For[domain.BatchRequest](nil)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		return nil, err
	}

	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = "Things CLI batch plan"
	schema["description"] = "Typed sequential mutations. Omitted patch fields are preserved; null clears supported fields. The entire plan is validated before dispatch. No atomicity or exactly-once delivery is promised."
	visit(schema)

	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetIndent("", "  ")
	if err := e.Encode(schema); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func visit(v any) {
	switch x := v.(type) {
	case map[string]any:
		if p, ok := x["properties"].(map[string]any); ok {
			for name, raw := range p {
				f, ok := raw.(map[string]any)
				if !ok {
					continue
				}

				switch name {
				case "action":
					f["enum"] = []string{"create_todo", "create_project", "update", "update_project", "set_status"}
				case "kind":
					f["enum"] = []string{"todo", "heading"}
				case "status":
					f["enum"] = []string{"incomplete", "completed", "canceled"}
				case "actions":
					f["minItems"] = 1
					f["maxItems"] = 250
				case "checklist":
					if items, ok := f["items"].(map[string]any); ok {
						if properties, ok := items["properties"].(map[string]any); ok {
							delete(properties, "id")
						}
					}
				case "operation_key":
					f["maxLength"] = 256
				}
			}
		}

		for _, child := range x {
			visit(child)
		}
	case []any:
		for _, child := range x {
			visit(child)
		}
	}
}

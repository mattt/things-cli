package domain

import "time"

const SchemaVersion = 1

type Meta struct {
	Backend    string            `json:"backend"`
	ObservedAt time.Time         `json:"observed_at"`
	NextCursor *string           `json:"next_cursor"`
	Ordering   string            `json:"ordering,omitempty"`
	Coverage   map[string]string `json:"coverage"`
}

type Response struct {
	SchemaVersion int    `json:"schema_version"`
	Data          any    `json:"data"`
	Meta          Meta   `json:"meta"`
	Error         *Error `json:"error,omitempty"`
}

func Wrap(data any, backend string, at time.Time) Response {
	if operation, ok := data.(Operation); ok {
		data = operation.publicResult()
	}
	return Response{
		SchemaVersion: SchemaVersion,
		Data:          data,
		Meta: Meta{
			Backend:    backend,
			ObservedAt: at,
			Coverage: map[string]string{
				"recurrence": "materialized_only",
				"sync":       "local_observation_only",
			},
		},
	}
}

// publicResult keeps verification baselines in the journal, outside CLI and MCP
// responses. Copy children before changing them so journal records stay intact.
func (o Operation) publicResult() Operation {
	o.BaselineIDs = nil
	if o.Children != nil {
		children := make([]Operation, len(o.Children))
		for n, child := range o.Children {
			children[n] = child.publicResult()
		}
		o.Children = children
	}
	return o
}

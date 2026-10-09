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

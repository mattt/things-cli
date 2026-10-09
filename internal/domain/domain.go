// Package domain defines the records, requests, operations, and errors
// shared by the CLI, the MCP server, and the service.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Exit    int    `json:"exit_code"`
}

func (e *Error) Error() string { return e.Message }

func E(code, message string, exit int) *Error { return &Error{code, message, exit} }

func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return E("internal_error", "An internal operation failed.", 1)
}

func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

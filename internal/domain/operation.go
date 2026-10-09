package domain

import (
	"fmt"
	"time"
)

type Evidence struct {
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
	Detail     string    `json:"detail"`
}

type Operation struct {
	ID                string      `json:"operation_id"`
	RequestKey        string      `json:"request_key,omitempty"`
	RequestHash       string      `json:"request_hash"`
	InputHash         string      `json:"input_hash,omitempty"`
	Request           Request     `json:"request"`
	State             string      `json:"state"`
	Outcome           string      `json:"outcome"`
	ItemIDs           []string    `json:"item_ids"`
	Evidence          []Evidence  `json:"evidence"`
	Errors            []*Error    `json:"errors"`
	BeforeFingerprint string      `json:"before_fingerprint,omitempty"`
	BaselineIDs       []string    `json:"baseline_ids,omitempty"`
	Children          []Operation `json:"children,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
	DryRun            bool        `json:"dry_run,omitempty"`
}

func (o Operation) ResultError() error {
	if o.DryRun || o.Outcome == "confirmed" {
		return nil
	}
	if o.Outcome == "partial" {
		return E("partial_batch", "The batch has partial results. Inspect its operations before retrying.", 8)
	}
	if o.Outcome == "failed" {
		if len(o.Errors) > 0 {
			return o.Errors[0]
		}
		return E("write_rejected", "The write was rejected.", 6)
	}
	return E("outcome_unknown", fmt.Sprintf("Operation %s is uncertain. Reconcile it before retrying.", o.ID), 7)
}

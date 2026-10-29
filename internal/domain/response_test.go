package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWrapOperationCopiesPrivateBaselines(t *testing.T) {
	child := Operation{ID: "child", BaselineIDs: []string{"private-child"}, ItemIDs: []string{"created"}, Outcome: "confirmed", Evidence: []Evidence{{Source: "state_observation"}}}
	original := Operation{ID: "parent", BaselineIDs: []string{"private-parent"}, Children: []Operation{child}}
	response := Wrap(original, "journal", time.Time{})
	b, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "private-") || strings.Contains(string(b), "baseline_ids") {
		t.Fatal("private baseline exposed")
	}
	public := response.Data.(Operation)
	if public.Children[0].ItemIDs[0] != "created" || public.Children[0].Outcome != "confirmed" || public.Children[0].Evidence[0].Source != "state_observation" {
		t.Fatal("verification result lost")
	}
	if len(original.BaselineIDs) != 1 || len(original.Children[0].BaselineIDs) != 1 {
		t.Fatal("journal operation was changed")
	}
}

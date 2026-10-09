package urlscheme

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/mattt/things-cli/internal/domain"
)

func text(v string) *string { return &v }

func TestEncodingAndClear(t *testing.T) {
	value := "Café & 100% + 🚀\nline\" $(touch /tmp/never)"
	r := domain.Request{
		Action: "update",
		ID:     "id",
		Patch:  domain.Patch{Title: text(value), Notes: text(value), Deadline: text("")},
	}
	raw, err := Build(r, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("title") != value || q.Get("notes") != value || q.Get("auth-token") != "secret" || !q.Has("deadline") || q.Get("deadline") != "" {
		t.Fatal("encoding changed input")
	}
}

func TestNestedProjectJSON(t *testing.T) {
	r := domain.Request{
		Action: "create_project",
		Patch:  domain.Patch{Title: text("P"), Area: text("area")},
		Items: []domain.ProjectChild{
			{Kind: "heading", Patch: domain.Patch{Title: text("H")}},
			{Kind: "todo", Patch: domain.Patch{Title: text("T")}},
		},
	}
	raw, err := Build(r, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(raw)
	var objects []object
	if err := json.Unmarshal([]byte(u.Query().Get("data")), &objects); err != nil {
		t.Fatal(err)
	}

	if u.Path != "/json" || len(objects) != 1 || objects[0].Attributes["area-id"] != "area" {
		t.Fatal(objects)
	}
	if _, ok := objects[0].Attributes["list-id"]; ok {
		t.Fatal("project used todo destination")
	}
}

func FuzzURLRoundTrip(f *testing.F) {
	for _, s := range []string{
		"plain",
		"&%+",
		"🚀\nnotes",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		raw, err := Build(domain.Request{Action: "create_todo", Patch: domain.Patch{Title: &value}}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(raw)
		if err != nil || u.Query().Get("title") != value {
			t.Fatal("failed round trip")
		}
	})
}

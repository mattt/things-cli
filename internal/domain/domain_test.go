package domain

import "testing"

func TestPatchAbsentAndNull(t *testing.T) {
	var p Patch
	if err := StrictDecode([]byte(`{"notes":null,"deadline":"","tags":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Title != nil || p.Notes == nil || *p.Notes != "" || p.Deadline == nil || p.Tags == nil || len(*p.Tags) != 0 {
		t.Fatalf("incorrect patch: %+v", p)
	}

	if err := StrictDecode([]byte(`{"priority":1}`), &p); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func FuzzStructuredRequest(f *testing.F) {
	f.Add(`{"action":"update","id":"task","patch":{"notes":null}}`)
	f.Add(`{"action":"create_todo","patch":{"title":"Hi 🚀"}}`)
	f.Fuzz(func(t *testing.T, value string) {
		var r Request
		_ = StrictDecode([]byte(value), &r)
	})
}

package urlscheme

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
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

func TestDispatchKeepsURLAndTokenOutOfArguments(t *testing.T) {
	raw, err := Build(domain.Request{Action: "update", ID: "task", Patch: domain.Patch{Title: text("Private notes; $(touch /tmp/never)")}}, "synthetic-private-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatchCommand(context.Background(), raw)
	for _, arg := range command.Args {
		if strings.Contains(arg, "synthetic-private-token") || strings.Contains(arg, raw) || strings.Contains(arg, "Private notes") {
			t.Fatal("sensitive URL content appeared in process arguments")
		}
	}
	if command.Path != "/usr/bin/osascript" || command.Env != nil {
		t.Fatal(command)
	}
	input, err := io.ReadAll(command.Stdin)
	if err != nil || string(input) != raw {
		t.Fatal("URL pipe changed input", err)
	}
}

func TestNativeDispatchBridgeReadsStandardInput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS JavaScript for Automation bridge")
	}
	// Exercise the native stdin and NSURL conversion without opening any URL.
	const open = "if (!$.NSWorkspace.sharedWorkspace.openURL($.NSURL.URLWithString(value))) {\n    throw new Error('URL dispatch failed');\n}"
	script := strings.Replace(dispatchScript, open, "ObjC.unwrap($.NSURL.URLWithString(value).absoluteString);", 1)
	if script == dispatchScript {
		t.Fatal("native test did not replace URL opening")
	}
	raw := "things:///update?id=synthetic&auth-token=synthetic%20token&notes=%22%3B%20throw%20new%20Error()"
	command := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script)
	command.Stdin = strings.NewReader(raw)
	output, err := command.Output()
	if err != nil || strings.TrimSpace(string(output)) != raw {
		t.Fatalf("bridge changed URL: %q (%v)", output, err)
	}
}

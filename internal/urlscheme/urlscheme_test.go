package urlscheme

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
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

func TestDispatchActivatesThingsOnlyToShowItems(t *testing.T) {
	writes := []domain.Request{
		{Action: "create_todo", Patch: domain.Patch{Title: text("Synthetic"), When: text("someday")}},
		{Action: "create_todo", Patch: domain.Patch{Title: text("Synthetic"), Checklist: &[]domain.ChecklistItem{{Title: "Step"}}}},
		{Action: "create_project", Patch: domain.Patch{Title: text("Synthetic")}},
		{Action: "update", ID: "task", Patch: domain.Patch{Deadline: text("2026-11-30")}},
		{Action: "update_project", ID: "project", Patch: domain.Patch{Notes: text("Synthetic")}},
	}
	for _, r := range writes {
		raw, err := Build(r, "synthetic-token", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := dispatchCommand(context.Background(), raw).Args; got[len(got)-1] != "background" {
			t.Fatalf("%s dispatch arguments = %q", r.Action, got)
		}
	}
	if got := dispatchCommand(context.Background(), Show("task")).Args; got[len(got)-1] != "activate" {
		t.Fatalf("show dispatch arguments = %q", got)
	}
}

func TestNativeDispatchBridgeReadsStandardInput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS JavaScript for Automation bridge")
	}
	// Exercise the native stdin, NSURL conversion, and launch options without
	// opening any URL.
	script := strings.Replace(dispatchScript, openURL, "(console.log(ObjC.unwrap(url.absoluteString) + ' ' + options) || true)", 1)
	if script == dispatchScript {
		t.Fatal("native test did not replace URL opening")
	}
	run := func(raw, mode string) (string, error) {
		command := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script, mode)
		command.Stdin = strings.NewReader(raw)
		output, err := command.CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}

	raw := "things:///update?id=synthetic&auth-token=synthetic%20token&notes=%22%3B%20throw%20new%20Error()"
	for mode, options := range map[string]string{"background": "66048", "activate": "65536"} {
		output, err := run(raw, mode)
		if err != nil || output != raw+" "+options {
			t.Fatalf("%s bridge changed URL or options: %q (%v)", mode, output, err)
		}
	}
	// A URL without a scheme fails before Launch Services sees it.
	if output, err := run("not a things url", "background"); err == nil || strings.Contains(output, "not") {
		t.Fatalf("bridge accepted a URL without a scheme: %q", output)
	}
}

func TestDispatchProcessFailures(t *testing.T) {
	// Failure to start proves that the helper could not send a URL.
	command := exec.Command("/things-cli-nonexistent/osascript")
	if e := domain.AsError(runDispatch(command)); e.Code != "not_dispatched" || e.Exit != 6 {
		t.Fatal(e)
	}
	// An unsuccessful exit after start cannot prove that no URL was sent.
	command = exec.Command("/bin/sh", "-c", "exit 1")
	if e := domain.AsError(runDispatch(command)); e.Code != "dispatch_failed" || e.Exit != 7 {
		t.Fatal(e)
	}
}

func TestSandboxDispatchStartFailure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/usr/bin/sandbox-exec", "-p", `(version 1)(allow default)(deny process-exec (literal "/usr/bin/osascript"))`, executable, "-test.run=^TestSandboxDispatchHelper$")
	command.Env = append(os.Environ(), "THINGS_TEST_SANDBOX_DISPATCH=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sandbox dispatch classification: %v: %s", err, output)
	}
}

func TestSandboxDispatchHelper(t *testing.T) {
	if os.Getenv("THINGS_TEST_SANDBOX_DISPATCH") != "1" {
		t.Skip("subprocess helper")
	}
	err := (System{}).Dispatch(context.Background(), "things-cli-diagnostic-nonexistent:///probe")
	if e := domain.AsError(err); e.Code != "not_dispatched" || e.Exit != 6 {
		t.Fatal(e)
	}
}

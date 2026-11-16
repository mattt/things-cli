package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/testutil"
)

func TestCLIJSONAndExitCodes(t *testing.T) {
	db, path := testutil.Database(t)
	testutil.Exec(t, db, `INSERT INTO TMTask(uuid,title) VALUES ('task','A'); INSERT INTO TMTask(uuid,title) VALUES ('task-two','B')`)

	cases := []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"list", "inbox", "--json"}, 0, ""},
		{[]string{"get", "task", "--json"}, 0, ""},
		{[]string{"get", "task-", "--json"}, 0, ""},
		{[]string{"get", "missing", "--json"}, 3, ""},
		{[]string{"update", "task", "--notes", "x", "--dry-run", "--json"}, 0, ""},
		{[]string{"list", "bad", "--json"}, 2, ""},
		{[]string{"list", "inbox", "--limit", "0", "--json"}, 2, ""},
		{[]string{"version", "--json"}, 0, ""},
		{[]string{"complete", "task", "--completion-date", "2999-01-01", "--dry-run", "--json"}, 2, "future_completion_date"},
		{[]string{"complete", "task", "--completion-date", "soon", "--dry-run", "--json"}, 2, "invalid_completion_date"},
		{[]string{"update", "task", "--completion-date", "2026-01-15", "--dry-run", "--json"}, 2, "completion_date_requires_closed_status"},
		{[]string{"add", "Done", "--completion-date", "2026-01-15", "--dry-run", "--json"}, 2, "completion_date_requires_closed_status"},
		{[]string{"add", "Done", "--completed", "--canceled", "--dry-run", "--json"}, 2, "conflicting_status"},
	}

	for _, c := range cases {
		args := append([]string{"--database", path, "--config-dir", t.TempDir()}, c.args...)
		var out, stderr bytes.Buffer
		exit := Run(context.Background(), args, &out, &stderr)
		if exit != c.exit {
			t.Fatalf("%v: exit %d want %d; %s", c.args, exit, c.exit, stderr.String())
		}

		var r domain.Response
		if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.SchemaVersion != 1 {
			t.Fatalf("invalid JSON %s: %v", out.String(), err)
		}
		if c.code != "" && (r.Error == nil || r.Error.Code != c.code) {
			t.Fatalf("%v: got %s, want error %s", c.args, out.String(), c.code)
		}
	}
}

func TestCompletionDateDryRuns(t *testing.T) {
	db, path := testutil.Database(t)
	testutil.Exec(t, db, `INSERT INTO TMTask(uuid,title) VALUES ('task','A')`)

	cases := []struct {
		args         []string
		status, date string
	}{
		{[]string{"complete", "task", "--completion-date", "2026-01-15"}, "completed", time.Date(2026, 1, 15, 0, 0, 0, 0, time.Local).Format(time.RFC3339)},
		{[]string{"cancel", "task", "--completion-date", "2026-01-15T09:30:00Z"}, "canceled", time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC).In(time.Local).Format(time.RFC3339)},
		{[]string{"add", "Done", "--completed", "--completion-date", "2026-01-15T08:15"}, "completed", time.Date(2026, 1, 15, 8, 15, 0, 0, time.Local).Format(time.RFC3339)},
		{[]string{"add", "Done", "--canceled", "--completion-date", "2026-01-15T08:15:30+02:00"}, "canceled", time.Date(2026, 1, 15, 6, 15, 30, 0, time.UTC).In(time.Local).Format(time.RFC3339)},
	}
	for _, c := range cases {
		args := append([]string{"--database", path, "--config-dir", t.TempDir()}, c.args...)
		args = append(args, "--dry-run", "--json")
		var out, stderr bytes.Buffer
		if exit := Run(context.Background(), args, &out, &stderr); exit != 0 {
			t.Fatalf("%v: exit %d; %s %s", c.args, exit, out.String(), stderr.String())
		}

		var r struct {
			Data domain.Operation `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		p := r.Data.Request.Patch
		if p.Status == nil || *p.Status != c.status || p.CompletionDate == nil || *p.CompletionDate != c.date {
			t.Fatalf("%v: got %s", c.args, out.String())
		}
	}
}

func BenchmarkCLIColdStart10000(b *testing.B) {
	binary := os.Getenv("THINGS_TEST_BINARY")
	if binary == "" {
		b.Skip("make benchmark supplies the built CLI")
	}

	db, path := testutil.Database(b)
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare("INSERT INTO TMTask(uuid,title,start,startDate) VALUES(?,?,1,?)")
	if err != nil {
		b.Fatal(err)
	}

	date := testutil.Date(time.Now().Format("2006-01-02"))
	for n := 0; n < 10000; n++ {
		if _, err := stmt.Exec(time.Unix(int64(n), 0).Format(time.RFC3339), "Synthetic task", date); err != nil {
			b.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		command := exec.Command(binary, "--database", path, "list", "today", "--limit", "50", "--json")
		if err := command.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBlockedReadJSON(t *testing.T) {
	var out, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--database", t.TempDir() + "/missing", "list", "inbox", "--json"}, &out, &stderr)
	if exit != 4 {
		t.Fatal(exit, stderr.String())
	}

	var r domain.Response
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Error == nil || r.Data != nil {
		t.Fatal("blocked read became empty success", out.String())
	}
}

func TestSchedulingHelpMatchesSupportedValues(t *testing.T) {
	for _, args := range [][]string{{"add"}, {"project", "add"}, {"update"}, {"project", "update"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output, stderr bytes.Buffer
			help := append(append([]string{}, args...), "--help")
			if exit := Run(context.Background(), help, &output, &stderr); exit != 0 {
				t.Fatal(exit, stderr.String())
			}
			text := output.String()
			update := args[len(args)-1] == "update"
			if strings.Contains(text, "anytime") == update {
				t.Fatal(text)
			}
			if update && !strings.Contains(text, "use --clear-when") {
				t.Fatal(text)
			}
		})
	}
}

func TestDatabaseEnvironmentVariable(t *testing.T) {
	db, path := testutil.Database(t)
	testutil.Exec(t, db, `INSERT INTO TMTask(uuid,title) VALUES ('task','Selected by THINGS_DATABASE')`)
	// Without a Things container in HOME, only the variable can select the fixture.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("THINGS_DATABASE", path)

	var out, stderr bytes.Buffer
	if exit := Run(context.Background(), []string{"--config-dir", t.TempDir(), "list", "inbox", "--json"}, &out, &stderr); exit != 0 {
		t.Fatal(exit, stderr.String())
	}
	if !strings.Contains(out.String(), "Selected by THINGS_DATABASE") {
		t.Fatal("did not read the selected database", out.String())
	}

	out.Reset()
	stderr.Reset()
	if exit := Run(context.Background(), []string{"--database", t.TempDir() + "/missing", "--config-dir", t.TempDir(), "list", "inbox", "--json"}, &out, &stderr); exit != 4 {
		t.Fatal("flag did not take precedence", exit, out.String())
	}
}

func TestMCPConfigKeepsSelectedDatabase(t *testing.T) {
	t.Setenv("THINGS_DATABASE", "/selected/main.sqlite")

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"mcp", "config"}, "/selected/main.sqlite"},
		{[]string{"--database", "/flag/main.sqlite", "mcp", "config"}, "/flag/main.sqlite"},
	} {
		var out, stderr bytes.Buffer
		if exit := Run(context.Background(), c.args, &out, &stderr); exit != 0 {
			t.Fatal(exit, stderr.String())
		}
		var config struct {
			MCPServers map[string]struct {
				Args []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(out.Bytes(), &config); err != nil {
			t.Fatal(err, out.String())
		}
		args := config.MCPServers["things"].Args
		if len(args) < 2 || args[0] != "--database" || args[1] != c.want {
			t.Fatal(c.args, args)
		}
	}
}

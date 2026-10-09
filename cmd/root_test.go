package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
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
	}{
		{[]string{"list", "inbox", "--json"}, 0},
		{[]string{"get", "task", "--json"}, 0},
		{[]string{"get", "task-", "--json"}, 0},
		{[]string{"get", "missing", "--json"}, 3},
		{[]string{"update", "task", "--notes", "x", "--dry-run", "--json"}, 0},
		{[]string{"list", "bad", "--json"}, 2},
		{[]string{"list", "inbox", "--limit", "0", "--json"}, 2},
		{[]string{"version", "--json"}, 0},
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

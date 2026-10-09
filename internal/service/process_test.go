package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/journal"
	"github.com/mattt/things-cli/internal/readstore"
	"github.com/mattt/things-cli/internal/testutil"
)

func TestProcessMutationHelper(t *testing.T) {
	if os.Getenv("THINGS_SYNTHETIC_PROCESS") != "1" {
		t.Skip("subprocess helper")
	}

	path := os.Getenv("THINGS_SYNTHETIC_DATABASE")
	dir := os.Getenv("THINGS_SYNTHETIC_JOURNAL")
	j, err := journal.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := &Service{
		Reader:  readstore.Lazy{Path: path},
		Journal: j,
		Tokens:  token("synthetic-token"),
		Dispatcher: dispatchFunc(func(_ context.Context, raw string) error {
			u, _ := url.Parse(raw)
			f, err := os.OpenFile(filepath.Join(dir, "dispatch-count"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}

			_, err = f.WriteString("dispatch\n")
			f.Close()
			if err != nil {
				return err
			}

			_, err = db.Exec("UPDATE TMTask SET title=? WHERE uuid=?", u.Query().Get("title"), u.Query().Get("id"))
			return err
		}),
	}

	o, err := s.Mutate(context.Background(), domain.Request{
		Action:       "update",
		ID:           "task",
		Patch:        domain.Patch{Title: str("After")},
		OperationKey: "process-key",
	})
	if err != nil || o.Outcome != "confirmed" {
		t.Fatal(o, err)
	}

	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("THINGS_SYNTHETIC_RESULT"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTwoProcessesShareReplayProtection(t *testing.T) {
	db, path := testutil.Database(t)
	testutil.Exec(t, db, "INSERT INTO TMTask(uuid,title) VALUES('task','Before')")
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := []string{filepath.Join(dir, "first.json"), filepath.Join(dir, "second.json")}
	for _, result := range results {
		wg.Go(func() {
			command := exec.Command(exe, "-test.run=^TestProcessMutationHelper$")
			command.Env = append(os.Environ(),
				"THINGS_SYNTHETIC_PROCESS=1",
				"THINGS_SYNTHETIC_DATABASE="+path,
				"THINGS_SYNTHETIC_JOURNAL="+dir,
				"THINGS_SYNTHETIC_RESULT="+result,
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Error(err, string(output))
			}
		})
	}
	wg.Wait()

	var first string
	for _, result := range results {
		b, err := os.ReadFile(result)
		if err != nil {
			t.Fatal(err)
		}

		var o domain.Operation
		if err := json.Unmarshal(b, &o); err != nil {
			t.Fatal(err)
		}

		if first == "" {
			first = o.ID
		} else if o.ID != first {
			t.Fatal("processes used different operation IDs")
		}
	}

	log, err := os.ReadFile(filepath.Join(dir, "dispatch-count"))
	if err != nil || string(log) != "dispatch\n" {
		t.Fatal("write was dispatched more than once", string(log), err)
	}
}

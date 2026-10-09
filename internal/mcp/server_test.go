package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/journal"
	"github.com/mattt/things-cli/internal/readstore"
	"github.com/mattt/things-cli/internal/service"
	"github.com/mattt/things-cli/internal/testutil"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type noDispatch struct{}

func (noDispatch) Dispatch(context.Context, string) error { return nil }

func TestMCPInitializationToolsAndOutcomes(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		t.Run(map[bool]string{true: "read_only", false: "writes"}[readOnly], func(t *testing.T) {
			db, path := testutil.Database(t)
			testutil.Exec(t, db, `INSERT INTO TMTask(uuid,title,start) VALUES ('task','Task',0)`)
			j, err := journal.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()

			s := &service.Service{
				Reader:        readstore.Lazy{Path: path},
				Journal:       j,
				Dispatcher:    noDispatch{},
				VerifyTimeout: 10 * time.Millisecond,
				PollInterval:  time.Millisecond,
			}
			server := New(s, readOnly)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			st, ct := sdk.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()

			client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
			cs, err := client.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()

			tools, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}

			names := map[string]bool{}
			for _, tool := range tools.Tools {
				names[tool.Name] = true
				if tool.InputSchema == nil {
					t.Fatal("missing schema")
				}
			}
			if readOnly && names["create_todo"] {
				t.Fatal("read-only exposed creation")
			}
			if !names["list_items"] || !names["get_operation"] {
				t.Fatal(names)
			}

			result, err := cs.CallTool(ctx, &sdk.CallToolParams{
				Name:      "list_items",
				Arguments: map[string]any{"list": "inbox", "limit": 1},
			})
			if err != nil || result.IsError || result.StructuredContent == nil {
				t.Fatal(result, err)
			}

			b, _ := json.Marshal(result.StructuredContent)
			var r domain.Response
			if err := json.Unmarshal(b, &r); err != nil || r.SchemaVersion != 1 {
				t.Fatal(string(b), err)
			}

			if !readOnly {
				result, err = cs.CallTool(ctx, &sdk.CallToolParams{
					Name:      "create_todo",
					Arguments: map[string]any{"patch": map[string]any{"title": "New"}, "operation_key": "mcp-once"},
				})
				if err != nil || !result.IsError || result.StructuredContent == nil {
					t.Fatal("unknown write conveyed success", result, err)
				}

				b, _ := json.Marshal(result.StructuredContent)
				var envelope struct {
					Data domain.Operation `json:"data"`
				}
				if err := json.Unmarshal(b, &envelope); err != nil || envelope.Data.Outcome != "unknown" {
					t.Fatal(string(b), err)
				}
			}
		})
	}
}

func TestMCPStdio(t *testing.T) {
	binary := os.Getenv("THINGS_TEST_BINARY")
	if binary == "" {
		t.Skip("make test supplies a built CLI for the stdio check")
	}

	db, path := testutil.Database(t)
	testutil.Exec(t, db, `INSERT INTO TMTask(uuid,title) VALUES ('task','Synthetic task')`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := exec.Command(binary, "--database", path, "--config-dir", t.TempDir(), "mcp", "--read-only")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err, stderr.String())
	}

	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "list_items",
		Arguments: map[string]any{"list": "inbox"},
	})
	// Close waits for the process, so stderr is complete before the checks read it.
	if err := session.Close(); err != nil {
		t.Fatal(err, stderr.String())
	}
	if err != nil || result.IsError {
		t.Fatal(result, err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatal("unexpected diagnostics", stderr.String())
	}
}

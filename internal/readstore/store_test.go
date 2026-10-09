package readstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/testutil"
)

func TestSnapshotReadOnlyWAL(t *testing.T) {
	db, path := testutil.Database(t)
	testutil.Exec(t, db, "PRAGMA journal_mode=WAL")
	testutil.Exec(t, db,
		`INSERT INTO TMArea(uuid,title) VALUES ('area','Work'); INSERT INTO TMTag(uuid,title) VALUES ('tag','work'); INSERT INTO TMTask(uuid,type,title,area) VALUES ('project',1,'Project','area'); INSERT INTO TMTask(uuid,type,title,project) VALUES ('heading',2,'Heading','project'); INSERT INTO TMTask(uuid,title,heading,start,startDate,deadline,reminderTime) VALUES ('task','Task','heading',1,?,?,?);`,
		testutil.Date("2026-10-08"), testutil.Date("2026-10-16"), int64(12<<26|34<<20),
	)
	testutil.Exec(t, db,
		`INSERT INTO TMTaskTag VALUES ('task','tag'); INSERT INTO TMChecklistItem(uuid,title,status,task) VALUES ('row','Done',3,'task')`,
	)

	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	snap, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Item
	for _, i := range snap.Items {
		if i.ID == "task" {
			task = i
		}
	}
	if task.ProjectID != "project" || task.AreaID != "area" ||
		task.StartDate != "2026-10-08" || task.Deadline != "2026-10-16" || task.Reminder != "12:34" ||
		len(task.Tags) != 1 || len(task.Checklist) != 1 || task.Checklist[0].Status != "completed" {
		t.Fatalf("incorrect item: %+v", task)
	}

	if _, err := s.db.Exec("UPDATE TMTask SET title='Forbidden'"); err == nil {
		t.Fatal("write succeeded on read-only connection")
	}

	testutil.Exec(t, db, "UPDATE TMTask SET title=? WHERE uuid=?", "WAL update", "task")
	fresh, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range fresh.Items {
		if i.ID == "task" && i.Title != "WAL update" {
			t.Fatal("fresh snapshot missed WAL update")
		}
	}
}

func TestUnknownSchemaAndDecodeFail(t *testing.T) {
	for _, query := range []string{
		`UPDATE Meta SET value='999'`,
		`ALTER TABLE TMTask DROP COLUMN deadline`,
		`INSERT INTO TMTask(uuid,type) VALUES ('unknown',99)`,
		`INSERT INTO TMTask(uuid,startDate) VALUES ('bad-date',1)`,
	} {
		t.Run(query, func(t *testing.T) {
			db, path := testutil.Database(t)
			testutil.Exec(t, db, query)

			s, err := Open(context.Background(), path)
			if err == nil {
				defer s.Close()
				_, err = s.Snapshot(context.Background())
			}
			if err == nil || domain.AsError(err).Exit != 5 {
				t.Fatalf("expected explicit capability error: %v", err)
			}
		})
	}
}

func TestMissingDatabaseDoesNotBecomeEmpty(t *testing.T) {
	_, err := Open(context.Background(), t.TempDir()+"/missing.sqlite")
	if err == nil || domain.AsError(err).Exit != 4 {
		t.Fatalf("expected access error: %v", err)
	}
}

func TestDateAndReminderDecoding(t *testing.T) {
	date, err := decodeDate(sql.NullInt64{Int64: testutil.Date("2024-02-29"), Valid: true})
	if err != nil || date != "2024-02-29" {
		t.Fatal(date, err)
	}

	rem, err := decodeTime(sql.NullInt64{Int64: 0, Valid: true})
	if err != nil || rem != "00:00" {
		t.Fatal(rem, err)
	}
}

func TestWriteDatabaseSelectionAndIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, backup := testutil.Database(t)
	active := filepath.Join(home, "Library", "Group Containers", "JLMPQHK86H.com.culturedcode.ThingsMac", "ThingsData-test", "Things Database.thingsdatabase", "main.sqlite")
	if err := os.MkdirAll(filepath.Dir(active), 0700); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := (Lazy{Path: backup}).CheckWriteDatabase(); err == nil || domain.AsError(err).Code != "write_database_mismatch" {
		t.Fatal(err)
	}
	if err := (Lazy{Path: active}).CheckWriteDatabase(); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(home, "alias.sqlite")
	if err := os.Symlink(active, alias); err != nil {
		t.Fatal(err)
	}
	if err := (Lazy{Path: alias}).CheckWriteDatabase(); err != nil {
		t.Fatal(err)
	}
	first, err := (Lazy{Path: active}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := (Lazy{Path: alias}).Snapshot(context.Background())
	if err != nil || first.Database == "" || first.Database != second.Database {
		t.Fatal(first.Database, second.Database, err)
	}
	if err := os.Rename(active, active+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, content, 0600); err != nil {
		t.Fatal(err)
	}
	second, err = (Lazy{Path: active}).Snapshot(context.Background())
	if err != nil || first.Database == second.Database {
		t.Fatal("replacement retained identity", err)
	}
}

func TestOpenVersionRange(t *testing.T) {
	for v, ok := range map[int]bool{21: false, 22: true, 29: true, 30: false} {
		db, path := testutil.Database(t)
		testutil.Exec(t, db, "UPDATE Meta SET value=? WHERE key='databaseVersion'",
			fmt.Sprintf(`<?xml version="1.0"?><plist><integer>%d</integer></plist>`, v))

		s, err := Open(context.Background(), path)
		if ok != (err == nil) {
			t.Fatalf("version %d: accepted=%v, want %v (%v)", v, err == nil, ok, err)
		}
		if err == nil {
			s.Close()
		}
	}
}

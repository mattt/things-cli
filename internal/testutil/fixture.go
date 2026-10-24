// Package testutil creates synthetic Things databases. It never discovers a live library.
package testutil

import (
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed testdata/v24.sql
var Schema string

func Database(t testing.TB) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func Exec(t testing.TB, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func Date(value string) int64 {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return int64(t.Year()<<16 | int(t.Month())<<12 | t.Day()<<7)
}

// Discoverable places a synthetic database in an isolated Things container.
func Discoverable(t testing.TB, path string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	active := filepath.Join(home, "Library", "Group Containers", "JLMPQHK86H.com.culturedcode.ThingsMac", "ThingsData-test", "Things Database.thingsdatabase", "main.sqlite")
	if err := os.MkdirAll(filepath.Dir(active), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, active); err != nil {
		t.Fatal(err)
	}
}

package readstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mattt/things-cli/internal/domain"
)

func thingsRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Library", "Group Containers", "JLMPQHK86H.com.culturedcode.ThingsMac")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDiscoverIgnoresStubAtOldBundlePath(t *testing.T) {
	root := thingsRoot(t)
	if err := os.WriteFile(filepath.Join(root, "Things Database.thingsdatabase"), []byte("moved"), 0600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "ThingsData-test", "Things Database.thingsdatabase", "main.sqlite")
	if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Discover("")
	if err != nil || got != want {
		t.Fatal(got, err)
	}
}

func TestDiscoverUnreadableContainer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := thingsRoot(t)
	if err := os.Chmod(root, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0700) })
	if _, err := Discover(""); err == nil || domain.AsError(err).Code != "database_access_denied" {
		t.Fatal(err)
	}
}

func TestDiscoverEmptyContainer(t *testing.T) {
	thingsRoot(t)
	_, err := Discover("")
	if err == nil || domain.AsError(err).Code != "database_missing" {
		t.Fatal(err)
	}
}

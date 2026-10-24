package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMakeRebuildsChangedVersion(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Use the real source and Makefile with an isolated output binary.
	for _, name := range []string{"Makefile", "cmd", "internal", "main.go", "go.mod", "go.sum"} {
		if err := os.Symlink(filepath.Join(root, name), filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []string{"dev", "0.1.0", "dev"} {
		value := version
		if value == "dev" {
			value = ""
		}
		command := exec.Command("make", "things", "VERSION="+value, "GO="+filepath.Join(runtime.GOROOT(), "bin", "go"))
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %s (%v)", version, output, err)
		}
		output, err := exec.Command(filepath.Join(dir, "things"), "version").Output()
		if err != nil || strings.TrimSpace(string(output)) != version {
			t.Fatalf("wanted %s, got %q (%v)", version, output, err)
		}
	}
}

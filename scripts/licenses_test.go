package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLicensesMatchCachedReleaseDependencies(t *testing.T) {
	if err := collect("..", true); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRequiresCompleteUnchangedLicenses(t *testing.T) {
	root := ".."
	manifest, err := os.ReadFile(filepath.Join(root, "licenses/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]string
	if err := json.Unmarshal(manifest, &entries); err != nil {
		t.Fatal(err)
	}
	entries["licenses/manifest.json"] = ""
	entries["README.md"] = ""
	entries["things"] = ""
	for _, defect := range []string{"none", "missing", "changed", "duplicate"} {
		t.Run(defect, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release.tar.gz")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			zipped := gzip.NewWriter(file)
			archive := tar.NewWriter(zipped)
			const target = "licenses/dependencies/modernc.org--sqlite@v1.60.1/LICENSE-3RD-PARTY.md"
			for name := range entries {
				if name == target && defect == "missing" {
					continue
				}
				content := []byte("synthetic binary")
				if name != "things" {
					content, err = os.ReadFile(filepath.Join(root, name))
					if err != nil {
						t.Fatal(err)
					}
				}
				if name == target && defect == "changed" {
					content = []byte("truncated license")
				}
				count := 1
				if name == target && defect == "duplicate" {
					count = 2
				}
				for range count {
					if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
						t.Fatal(err)
					}
					if _, err := archive.Write(content); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := zipped.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			err = checkArchive(root, path)
			if (err == nil) != (defect == "none") {
				t.Fatal(defect, err)
			}
		})
	}
}

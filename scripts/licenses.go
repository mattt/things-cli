// Command licenses collects local license files and checks release archives.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 1:
		err = collect(".", false)
	case len(os.Args) == 2 && os.Args[1] == "--check":
		err = collect(".", true)
	case len(os.Args) > 2 && os.Args[1] == "--archive":
		for _, path := range os.Args[2:] {
			if err = checkArchive(".", path); err != nil {
				break
			}
			fmt.Println("Checked", path)
		}
	default:
		err = fmt.Errorf("usage: go run ./scripts [--check | --archive ARCHIVE...]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func collect(root string, check bool) error {
	sources := map[string]string{"licenses/Go-LICENSE.txt": filepath.Join(runtime.GOROOT(), "LICENSE")}
	// Collect the union of modules linked into the two release binaries.
	for _, arch := range []string{"amd64", "arm64"} {
		command := exec.Command("go", "list", "-deps", "-json", ".")
		command.Dir = root
		command.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+arch, "CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off")
		output, err := command.Output()
		if err != nil {
			return fmt.Errorf("list cached release dependencies: %w", err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(output)))
		for {
			var pkg struct {
				Module *struct {
					Path, Version, Dir string
					Main               bool
				}
			}
			if err := decoder.Decode(&pkg); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
			m := pkg.Module
			if m == nil || m.Main {
				continue
			}
			entries, err := os.ReadDir(m.Dir)
			if err != nil {
				return err
			}
			found := false
			for _, entry := range entries {
				name := strings.ToUpper(entry.Name())
				if entry.IsDir() || !(strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "NOTICE") || strings.HasPrefix(name, "COPYING")) {
					continue
				}
				target := "licenses/dependencies/" + strings.ReplaceAll(m.Path, "/", "--") + "@" + m.Version + "/" + entry.Name()
				sources[target] = filepath.Join(m.Dir, entry.Name())
				found = true
			}
			if !found {
				return fmt.Errorf("no license found for %s", m.Path)
			}
		}
	}
	manifest := map[string]string{}
	for target, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		destination := filepath.Join(root, filepath.FromSlash(target))
		if check {
			actual, err := os.ReadFile(destination)
			if err != nil || string(actual) != string(data) {
				return fmt.Errorf("stale license: %s; run make licenses", target)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(destination, data, 0644); err != nil {
				return err
			}
		}
		manifest[target] = digest(data)
	}
	for _, path := range []string{"LICENSE.md", "THIRD_PARTY_NOTICES.md", "licenses/things.py-Apache-2.0.txt"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return err
		}
		manifest[path] = digest(data)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(root, "licenses/manifest.json")
	if check {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(data) {
			return fmt.Errorf("stale license manifest; run make licenses")
		}
		return nil
	}
	return os.WriteFile(path, data, 0644)
}

func checkArchive(root, path string) error {
	data, err := os.ReadFile(filepath.Join(root, "licenses/manifest.json"))
	if err != nil {
		return err
	}
	var want map[string]string
	if err := json.Unmarshal(data, &want); err != nil {
		return err
	}
	want["licenses/manifest.json"] = digest(data)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer zipped.Close()
	archive := tar.NewReader(zipped)
	seen := map[string]bool{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(header.Name, "./")
		if seen[name] {
			return fmt.Errorf("duplicate archive entry: %s", name)
		}
		seen[name] = true
		expected, ok := want[name]
		if !ok {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("license is not a regular file: %s", name)
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			return err
		}
		if digest(content) != expected {
			return fmt.Errorf("license digest mismatch: %s", name)
		}
	}
	for name := range want {
		if !seen[name] {
			return fmt.Errorf("archive is missing %s", name)
		}
	}
	for _, name := range []string{"things", "README.md"} {
		if !seen[name] {
			return fmt.Errorf("archive is missing %s", name)
		}
	}
	return nil
}

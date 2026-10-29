package readstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/mattt/things-cli/internal/domain"
)

func Discover(override string) (string, error) {
	if override != "" {
		p, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return p, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, "Library", "Group Containers", "JLMPQHK86H.com.culturedcode.ThingsMac")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", missing(root)
		}
		return "", inspectFailure(root, err)
	}

	paths := []string{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "ThingsData-") || !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "Things Database.thingsdatabase", "main.sqlite")
		found, err := exists(path)
		if err != nil {
			return "", inspectFailure(path, err)
		}
		if found {
			paths = append(paths, path)
		}
	}

	// On some machines the old bundle path is a regular file that points to the
	// relocated library, so a stat below it fails with ENOTDIR.
	old := filepath.Join(root, "Things Database.thingsdatabase", "main.sqlite")
	found, err := exists(old)
	if err != nil {
		return "", inspectFailure(old, err)
	}
	if found {
		paths = append(paths, old)
	}

	sort.Strings(paths)
	if len(paths) == 0 {
		return "", missing(root)
	}
	if len(paths) > 1 {
		return "", domain.E("database_ambiguous", "Multiple Things libraries were found. Select one with --database PATH.", 4)
	}
	return paths[0], nil
}

// exists reports whether path is present, treating a missing path or a
// non-directory parent as absent.
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	return false, err
}

func missing(root string) error {
	return domain.E("database_missing", "No Things database was found in "+root+". Open Things or use --database PATH.", 4)
}

// inspectFailure shows privacy advice only for permission errors.
func inspectFailure(path string, err error) error {
	if os.IsPermission(err) {
		return accessDenied("Cannot access the Things database.")
	}
	return domain.E("database_unreadable", "Cannot inspect "+path+": "+err.Error()+". Use --database PATH to select a library.", 4)
}

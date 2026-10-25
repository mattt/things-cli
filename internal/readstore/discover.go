package readstore

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

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
			return "", domain.E("database_missing", "No Things database was found. Open Things or use --database PATH.", 4)
		}
		return "", accessDenied("Cannot access the Things container.")
	}

	paths := []string{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "ThingsData-") || !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "Things Database.thingsdatabase", "main.sqlite")
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		} else if !os.IsNotExist(err) {
			return "", accessDenied("Cannot inspect all Things libraries.")
		}
	}

	old := filepath.Join(root, "Things Database.thingsdatabase", "main.sqlite")
	if _, err := os.Stat(old); err == nil {
		paths = append(paths, old)
	} else if !os.IsNotExist(err) {
		return "", accessDenied("Cannot inspect the Things database.")
	}

	sort.Strings(paths)
	if len(paths) == 0 {
		return "", domain.E("database_missing", "No Things database was found. Open Things or use --database PATH.", 4)
	}
	if len(paths) > 1 {
		return "", domain.E("database_ambiguous", "Multiple Things libraries were found. Select one with --database PATH.", 4)
	}
	return paths[0], nil
}

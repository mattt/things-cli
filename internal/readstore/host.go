package readstore

import (
	"path/filepath"
	"strings"

	"github.com/mattt/things-cli/internal/domain"
)

// macOS grants Things container access to the app responsible for a process.
// Claude Desktop starts servers through a helper that disclaims that
// responsibility, so its own grant never reaches them.
const disclaimer = "/Contents/Helpers/disclaimer"

func accessDenied(problem string) error {
	app, disclaimed := host(ancestors())
	var advice string
	switch {
	case disclaimed:
		advice = app + " starts things without passing on its privacy permissions, so granting " + app +
			" access does not help. Run things from a terminal or another app that has access."
	case app != "":
		advice = "Allow " + app + ` access to "Data shared by Things and affiliated apps" in System Settings > ` +
			"Privacy & Security > Files & Folders, or grant " + app + " Full Disk Access."
	default:
		advice = `Allow the app that launches things access to "Data shared by Things and affiliated apps" ` +
			"in System Settings > Privacy & Security > Files & Folders."
	}
	return domain.E("database_access_denied", problem+" "+advice, 4)
}

// host names the nearest app bundle among executable paths ordered from the
// parent outward, and reports whether a disclaiming helper launched things.
func host(paths []string) (string, bool) {
	for _, p := range paths {
		n := strings.Index(p, ".app/")
		if n < 0 {
			continue
		}
		return filepath.Base(p[:n]), strings.HasSuffix(p, disclaimer)
	}
	return "", false
}

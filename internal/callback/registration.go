package callback

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const BundleID = "co.dododo.things-cli.callback"

type Registration struct {
	Status string
	Detail string
}

func CheckRegistration(ctx context.Context) Registration {
	if runtime.GOOS != "darwin" {
		return Registration{"unavailable", "Callback URL handling requires macOS."}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// The optional native bridge can query Launch Services without opening an app.
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	bridges := []string{
		filepath.Join(filepath.Dir(exe), "callback-bridge"),
		filepath.Join(filepath.Dir(exe), "dist", "Things CLI Callback.app", "Contents", "MacOS", "callback-bridge"),
		filepath.Join(home, "Applications", "Things CLI Callback.app", "Contents", "MacOS", "callback-bridge"),
		"/Applications/Things CLI Callback.app/Contents/MacOS/callback-bridge",
	}
	for _, bridge := range bridges {
		if _, err := os.Stat(bridge); err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, bridge, "--check-registration").Output()
		if err != nil {
			return Registration{"missing", "The callback app is not registered. Open it once before enabling --callbacks."}
		}
		id := strings.TrimSpace(string(out))
		if id == BundleID {
			return Registration{"registered", "Things CLI Callback is the default handler. Enable --callbacks to receive acknowledgments."}
		}
		return Registration{"ambiguous", "A different app is the default callback handler. Resolve the scheme collision before enabling callbacks."}
	}

	binary := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	data, err := exec.CommandContext(ctx, binary, "-dump").Output()
	if err != nil || len(data) == 0 {
		return Registration{"unknown", "Cannot inspect Launch Services. Register the callback app before using --callbacks."}
	}

	handlers := map[string]bool{}
	for _, block := range strings.Split(string(data), "bundle id:") {
		if !strings.Contains(block, Scheme) {
			continue
		}

		id := ""
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "identifier:") {
				id = strings.TrimSpace(strings.TrimPrefix(line, "identifier:"))
				if n := strings.Index(id, " "); n >= 0 {
					id = id[:n]
				}
				break
			}
		}
		if id != "" {
			handlers[id] = true
		}
	}

	if len(handlers) == 1 && handlers[BundleID] {
		return Registration{"registered", "The callback URL scheme is registered to Things CLI Callback. Enable --callbacks to use it."}
	}
	if len(handlers) > 0 {
		return Registration{"ambiguous", "Another app handles the callback scheme. Resolve the collision before using callbacks."}
	}
	return Registration{"missing", "Open the optional Things CLI Callback app once to register it. Read-back remains available without it."}
}

package version

import (
	"runtime/debug"
	"strings"
)

// version is set with -ldflags -X for release builds.
var version string

func String() string {
	if version != "" {
		return version
	}

	// go install and builds from a Git checkout record the module version.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

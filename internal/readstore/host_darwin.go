package readstore

import (
	"bytes"
	"os"

	"golang.org/x/sys/unix"
)

// ancestors returns executable paths from the parent process outward.
func ancestors() []string {
	paths := []string{}
	for pid := os.Getppid(); pid > 1 && len(paths) < 32; {
		// kern.procargs2 starts with argc, followed by the executable path.
		b, err := unix.SysctlRaw("kern.procargs2", pid)
		if err != nil || len(b) < 4 {
			break
		}
		b = b[4:]
		if n := bytes.IndexByte(b, 0); n >= 0 {
			b = b[:n]
		}
		paths = append(paths, string(b))

		p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			break
		}
		pid = int(p.Eproc.Ppid)
	}
	return paths
}

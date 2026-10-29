package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mattt/things-cli/internal/callback"
	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/keychain"
	"github.com/mattt/things-cli/internal/readstore"
	"github.com/spf13/cobra"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func (a *app) addDoctor(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Check Things installation, database access, and optional capabilities",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			checks := []check{}
			var failure error

			if runtime.GOOS != "darwin" {
				checks = append(checks, check{"platform", "blocked", "Things requires macOS."})
				failure = domain.E("platform_unsupported", "Things requires macOS.", 5)
			} else {
				home, _ := os.UserHomeDir()
				installed := false
				for _, p := range []string{"/Applications/Things3.app", filepath.Join(home, "Applications", "Things3.app")} {
					if _, err := os.Stat(p); err == nil {
						installed = true
					}
				}

				if !installed {
					ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
					b, err := exec.CommandContext(ctx, "/usr/bin/mdfind", "kMDItemCFBundleIdentifier == 'com.culturedcode.ThingsMac'").Output()
					cancel()
					installed = err == nil && len(b) > 0
				}

				if installed {
					checks = append(checks, check{"things_installation", "ok", "Things 3 is installed."})
				} else {
					checks = append(checks, check{"things_installation", "blocked", "Install Things 3 and open it once."})
					failure = domain.E("things_missing", "Things 3 was not found.", 4)
				}
			}

			checks = append(checks, check{"url_scheme", "unknown", "Check Things > Settings > General > Enable Things URLs. Dispatch alone cannot establish enablement."})

			path, err := readstore.Discover(a.databasePath())
			if err != nil {
				e := domain.AsError(err)
				checks = append(checks, check{"database", "blocked", e.Message})
				failure = err
			} else {
				s, err := readstore.Open(c.Context(), path)
				if err != nil {
					checks = append(checks, check{"database", "blocked", domain.AsError(err).Message})
					failure = err
				} else {
					snap, err := s.Snapshot(c.Context())
					s.Close()
					if err != nil {
						checks = append(checks, check{"database", "blocked", domain.AsError(err).Message})
						failure = err
					} else {
						checks = append(checks, check{
							"database",
							"ok",
							fmt.Sprintf("Read access works; schema version %d; %d local records.", snap.Version, len(snap.Items)),
						})
					}
				}
			}

			if keychain.Available() {
				checks = append(checks, check{"keychain", "available", "macOS Keychain is available. Run auth status to check token access."})
			} else {
				checks = append(checks, check{"keychain", "unavailable", "macOS Keychain is unavailable."})
			}

			registration := callback.CheckRegistration(c.Context())
			checks = append(checks, check{"callback_helper", registration.Status, registration.Detail})

			r := domain.Wrap(checks, "diagnostics", time.Now().UTC())
			if failure != nil {
				r.Error = domain.AsError(failure)
			}

			if a.json {
				if err := a.write(c, r); err != nil {
					return err
				}
			} else {
				a.emitted = true
				for _, v := range checks {
					fmt.Fprintf(c.OutOrStdout(), "%s: %s. %s\n", v.Name, v.Status, v.Detail)
				}
			}
			return failure
		},
	})
}

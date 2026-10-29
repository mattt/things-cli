// Package cmd exposes the Cobra command tree.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattt/things-cli/internal/callback"
	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/journal"
	"github.com/mattt/things-cli/internal/keychain"
	"github.com/mattt/things-cli/internal/readstore"
	"github.com/mattt/things-cli/internal/service"
	"github.com/mattt/things-cli/internal/urlscheme"
	"github.com/mattt/things-cli/internal/version"
	"github.com/spf13/cobra"
)

type app struct {
	json                bool
	database, configDir string
	verifyTimeout       time.Duration
	callbacks           bool
	emitted             bool
	protocol            bool
	service             *service.Service
}

// databasePath returns the --database flag, or THINGS_DATABASE when the flag is empty.
func (a *app) databasePath() string {
	if a.database != "" {
		return a.database
	}
	return os.Getenv("THINGS_DATABASE")
}

func NewRoot() (*cobra.Command, *app) {
	a := &app{verifyTimeout: 10 * time.Second}
	root := &cobra.Command{
		Use:           "things",
		Short:         "Read and manage Things 3 from the command line",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	root.PersistentFlags().BoolVar(&a.json, "json", false, "Return one versioned JSON object")
	root.PersistentFlags().StringVar(&a.database, "database", "", "Select the Things main.sqlite database")
	root.PersistentFlags().StringVar(&a.configDir, "config-dir", "", "Use a private configuration and journal directory")
	root.PersistentFlags().DurationVar(&a.verifyTimeout, "verify-timeout", 10*time.Second, "Bound mutation read-back verification")
	root.PersistentFlags().BoolVar(&a.callbacks, "callbacks", false, "Use the registered optional Things CLI Callback app")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return domain.E("invalid_input", err.Error(), 2) })

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if a.json {
				return a.write(c, domain.Wrap(map[string]string{"version": version.String()}, "cli", time.Now().UTC()))
			}
			_, err := fmt.Fprintln(c.OutOrStdout(), version.String())
			return err
		},
	})

	a.addReads(root)
	a.addMutations(root)
	a.addAuth(root)
	a.addDoctor(root)
	a.addOperations(root)
	a.addMCP(root)
	a.addCallback(root)

	validateArgs(root)
	return root, a
}

func validateArgs(c *cobra.Command) {
	if c.Args != nil {
		original := c.Args
		c.Args = func(c *cobra.Command, args []string) error {
			if err := original(c, args); err != nil {
				return domain.E("invalid_input", err.Error(), 2)
			}
			return nil
		}
	}

	for _, child := range c.Commands() {
		validateArgs(child)
	}
}

func (a *app) getService(writes bool) (*service.Service, error) {
	if a.verifyTimeout <= 0 || a.verifyTimeout > 5*time.Minute {
		return nil, domain.E("invalid_timeout", "Verification timeout must be greater than zero and at most 5 minutes.", 2)
	}

	if a.service == nil {
		if writes && a.callbacks {
			state := callback.CheckRegistration(context.Background())
			if state.Status == "missing" || state.Status == "ambiguous" || state.Status == "unavailable" {
				return nil, domain.E("callback_helper_unavailable", state.Detail, 4)
			}
		}

		a.service = &service.Service{
			Reader:        readstore.Lazy{Path: a.databasePath()},
			Tokens:        keychain.Keychain{},
			Dispatcher:    urlscheme.System{},
			VerifyTimeout: a.verifyTimeout,
			Callbacks:     a.callbacks,
		}
	}

	if writes && a.service.Journal == nil {
		j, err := journal.Open(a.configDir)
		if err != nil {
			return nil, err
		}
		a.service.Journal = j
	}
	return a.service, nil
}

func (a *app) write(c *cobra.Command, r domain.Response) error {
	a.emitted = true
	if a.json {
		return writeJSON(c.OutOrStdout(), r)
	}
	return writeText(c.OutOrStdout(), r)
}

// read runs a service call that needs no journal and writes its response.
func (a *app) read(c *cobra.Command, call func(*service.Service) (domain.Response, error)) error {
	s, err := a.getService(false)
	if err != nil {
		return err
	}
	r, err := call(s)
	if err != nil {
		return err
	}
	return a.write(c, r)
}

func (a *app) mutation(c *cobra.Command, o domain.Operation, err error) error {
	if err != nil && o.ID == "" {
		return err
	}

	r := domain.Wrap(o, "urlscheme", time.Now().UTC())
	resultErr := o.ResultError()
	if err != nil {
		resultErr = err
	}
	if resultErr != nil {
		r.Error = domain.AsError(resultErr)
	}

	if err := a.write(c, r); err != nil {
		return err
	}
	return resultErr
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root, a := NewRoot()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)

	err := root.ExecuteContext(ctx)
	if a.service != nil && a.service.Journal != nil {
		defer a.service.Journal.Close()
	}
	if err == nil {
		return 0
	}

	var e *domain.Error
	if !errors.As(err, &e) {
		if strings.HasPrefix(err.Error(), "unknown command ") {
			e = domain.E("invalid_input", err.Error(), 2)
		} else {
			e = domain.AsError(err)
		}
	}

	if a.json && !a.emitted && !a.protocol {
		r := domain.Wrap(nil, "cli", time.Now().UTC())
		r.Error = e
		if err := writeJSON(stdout, r); err != nil {
			fmt.Fprintln(stderr, "Cannot write JSON output.")
			return 1
		}
	}

	fmt.Fprintf(stderr, "%s: %s\n", e.Code, safe(e.Message))
	return e.Exit
}

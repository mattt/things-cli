package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/keychain"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *app) addAuth(root *cobra.Command) {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Store the Things URL update token in macOS Keychain",
	}

	var stdin bool
	set := &cobra.Command{
		Use:   "set",
		Short: "Prompt for an update token without echo",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var token string
			if stdin {
				scanner := bufio.NewScanner(c.InOrStdin())
				scanner.Buffer(make([]byte, 4096), 4097)
				if !scanner.Scan() {
					return domain.E("token_required", "Read one token from stdin.", 2)
				}
				token = strings.TrimSpace(scanner.Text())
			} else {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return domain.E("terminal_required", "Use an interactive terminal or auth set --stdin.", 2)
				}

				fmt.Fprint(c.ErrOrStderr(), "Things update token: ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(c.ErrOrStderr())
				if err != nil {
					return domain.E("token_input", "Cannot read the token.", 4)
				}
				token = strings.TrimSpace(string(b))
			}

			if err := keychain.Set(c.Context(), token); err != nil {
				return err
			}

			return a.write(c, domain.Wrap(map[string]bool{"stored": true}, "keychain", time.Now().UTC()))
		},
	}
	set.Flags().BoolVar(&stdin, "stdin", false, "Read one token line from stdin for controlled automation")

	auth.AddCommand(
		set,
		&cobra.Command{
			Use:   "status",
			Short: "Check token availability without printing it",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				_, err := (keychain.Keychain{}).Token(c.Context())
				stored := err == nil
				if err != nil && domain.AsError(err).Code != "token_missing" {
					return err
				}

				source := "keychain"
				if os.Getenv("THINGS_AUTH_TOKEN") != "" {
					source = "environment"
				}

				return a.write(c, domain.Wrap(map[string]any{"available": stored, "source": source}, "keychain", time.Now().UTC()))
			},
		},
		&cobra.Command{
			Use:   "delete",
			Short: "Delete the stored update token",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				if err := keychain.Delete(c.Context()); err != nil {
					return err
				}
				return a.write(c, domain.Wrap(map[string]bool{"deleted": true}, "keychain", time.Now().UTC()))
			},
		},
	)

	root.AddCommand(auth)
}

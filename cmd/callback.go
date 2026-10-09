package cmd

import (
	"github.com/mattt/things-cli/internal/callback"
	"github.com/mattt/things-cli/internal/domain"
	"github.com/spf13/cobra"
)

// addCallback registers the hidden command that the callback app runs for each URL event.
func (a *app) addCallback(root *cobra.Command) {
	cb := &cobra.Command{
		Use:    "callback",
		Hidden: true,
	}

	cb.AddCommand(&cobra.Command{
		Use:    "receive URL",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := callback.Forward(args[0]); err != nil {
				return domain.E("callback_rejected", "The callback was invalid or no operation is waiting for it.", 3)
			}
			return nil
		},
	})

	root.AddCommand(cb)
}

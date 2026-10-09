package cmd

import (
	"time"

	"github.com/mattt/things-cli/internal/domain"
	"github.com/spf13/cobra"
)

func (a *app) addOperations(root *cobra.Command) {
	parent := &cobra.Command{
		Use:   "operations",
		Short: "Inspect and reconcile durable mutation outcomes",
	}

	for _, verb := range []string{"get", "reconcile"} {
		reconcile := verb == "reconcile"
		parent.AddCommand(&cobra.Command{
			Use:   verb + " OPERATION_ID",
			Short: verb + " an operation without replaying its write",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				s, err := a.getService(true)
				if err != nil {
					return err
				}
				o, err := s.Operation(c.Context(), args[0], reconcile)
				return a.mutation(c, o, err)
			},
		})
	}

	parent.AddCommand(&cobra.Command{
		Use:   "prune",
		Short: "Remove settled records older than 90 days, removing their replay protection",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			s, err := a.getService(true)
			if err != nil {
				return err
			}
			count, err := s.Journal.Prune(c.Context(), time.Now())
			if err != nil {
				return err
			}

			return a.write(c, domain.Wrap(map[string]any{
				"removed":          count,
				"retention_days":   90,
				"retry_protection": "Removed records no longer protect operation keys.",
			}, "journal", time.Now().UTC()))
		},
	})

	root.AddCommand(parent)
}

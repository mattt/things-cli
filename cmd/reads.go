package cmd

import (
	"github.com/mattt/things-cli/internal/domain"
	"github.com/mattt/things-cli/internal/service"
	"github.com/spf13/cobra"
)

func queryFlags(c *cobra.Command, q *service.Query) {
	c.PreRunE = func(c *cobra.Command, _ []string) error {
		if c.Flags().Changed("limit") && q.Limit == 0 {
			return domain.E("invalid_limit", "Limit must be between 1 and 500.", 2)
		}
		return nil
	}

	c.Flags().IntVar(&q.Limit, "limit", 50, "Maximum records (1 through 500)")
	c.Flags().StringVar(&q.Cursor, "cursor", "", "Continue an unchanged query")
	c.Flags().BoolVar(&q.All, "all", false, "Export all matching records")
	c.Flags().StringVar(&q.Project, "project", "", "Filter by project ID or unique name")
	c.Flags().StringVar(&q.Area, "area", "", "Filter by area ID or unique name")
	c.Flags().StringVar(&q.Tag, "tag", "", "Filter by tag ID or unique name")
	c.Flags().StringVar(&q.Status, "status", "", "Filter by incomplete, completed, canceled, or all")
}

func (a *app) addReads(root *cobra.Command) {
	var listQuery service.Query
	list := &cobra.Command{
		Use:   "list LIST",
		Short: "Read inbox, today, evening, upcoming, anytime, someday, logbook, or trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			listQuery.List = args[0]
			return a.read(c, func(s *service.Service) (domain.Response, error) {
				return s.List(c.Context(), listQuery)
			})
		},
	}
	queryFlags(list, &listQuery)
	root.AddCommand(list)

	var searchQuery service.Query
	search := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search item titles and notes",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if args[0] == "" {
				return domain.E("query_required", "A search query is required.", 2)
			}

			searchQuery.Query = args[0]
			return a.read(c, func(s *service.Service) (domain.Response, error) {
				return s.List(c.Context(), searchQuery)
			})
		},
	}
	queryFlags(search, &searchQuery)
	root.AddCommand(search)

	root.AddCommand(&cobra.Command{
		Use:   "get ID",
		Short: "Read an item by ID, unique ID prefix, or Things link",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.read(c, func(s *service.Service) (domain.Response, error) {
				return s.Get(c.Context(), args[0])
			})
		},
	})

	for _, kind := range []string{"project", "area", "tag"} {
		var q service.Query
		q.Kind = kind
		c := &cobra.Command{
			Use:   kind + "s",
			Short: "Enumerate " + kind + "s",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				return a.read(c, func(s *service.Service) (domain.Response, error) {
					return s.List(c.Context(), q)
				})
			},
		}
		queryFlags(c, &q)
		root.AddCommand(c)
	}

	root.AddCommand(&cobra.Command{
		Use:   "open ID",
		Short: "Reveal an item in Things",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.read(c, func(s *service.Service) (domain.Response, error) {
				return s.Open(c.Context(), args[0])
			})
		},
	})
}

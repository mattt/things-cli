package cmd

import (
	"os"

	"github.com/mattt/things-cli/internal/mcp"
	"github.com/spf13/cobra"
)

func (a *app) addMCP(root *cobra.Command) {
	var readOnly bool
	server := &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP protocol messages over stdio",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			a.protocol = true
			s, err := a.getService(true)
			if err != nil {
				return err
			}
			return mcp.Run(c.Context(), s, readOnly)
		},
	}
	server.Flags().BoolVar(&readOnly, "read-only", false, "Omit all mutation tools")

	config := &cobra.Command{
		Use:   "config",
		Short: "Print an MCP configuration snippet without modifying clients",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := os.Executable()
			if err != nil {
				return err
			}

			args := []string{}
			if database := a.databasePath(); database != "" {
				args = append(args, "--database", database)
			}
			if a.configDir != "" {
				args = append(args, "--config-dir", a.configDir)
			}
			if a.callbacks {
				args = append(args, "--callbacks")
			}
			args = append(args, "mcp")
			if readOnly {
				args = append(args, "--read-only")
			}

			return writeJSON(c.OutOrStdout(), map[string]any{
				"mcpServers": map[string]any{
					"things": map[string]any{
						"command": path,
						"args":    args,
					},
				},
			})
		},
	}
	config.Flags().BoolVar(&readOnly, "read-only", false, "Configure a read-only server")

	server.AddCommand(config)
	root.AddCommand(server)
}

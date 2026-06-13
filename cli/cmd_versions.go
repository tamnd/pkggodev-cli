package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) versionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions <module>",
		Short: "List all versions of a module",
		Long: `List all known versions of a Go module from the GOPROXY version list.

Versions are returned newest first. Use -n to cap the number of results.`,
		Args:    cobra.ExactArgs(1),
		Example: "  gopkg versions github.com/spf13/cobra\n  gopkg versions github.com/gin-gonic/gin -n 5 -o table",
		RunE: func(cmd *cobra.Command, args []string) error {
			module := args[0]
			n := a.effectiveLimit(0)
			a.progressf("fetching versions for %s...", module)
			versions, err := a.client.Versions(cmd.Context(), module, n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(versions, len(versions))
		},
	}
}

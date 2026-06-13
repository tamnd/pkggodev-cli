package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) searchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search packages on pkg.go.dev",
		Long: `Search pkg.go.dev for Go packages matching query.

Results are sorted by relevance as returned by the site. Each record includes
the import path, a short synopsis, latest version, and publish date.`,
		Args:    cobra.MinimumNArgs(1),
		Example: "  gopkg search \"http server\" -n 10\n  gopkg search gin -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			n := a.effectiveLimit(10)
			a.progressf("searching pkg.go.dev for %q...", query)
			pkgs, err := a.client.Search(cmd.Context(), query, n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(pkgs, len(pkgs))
		},
	}
}

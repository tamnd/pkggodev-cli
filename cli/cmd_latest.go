package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) latestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "latest <module>",
		Short: "Show the latest version of a module",
		Long: `Show the latest published version of a Go module from the GOPROXY.

Returns the version string, publish timestamp, and a link to the pkg.go.dev
page for that version.`,
		Args:    cobra.ExactArgs(1),
		Example: "  gopkg latest github.com/spf13/cobra\n  gopkg latest golang.org/x/net -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			module := args[0]
			a.progressf("fetching latest version for %s...", module)
			info, err := a.client.Latest(cmd.Context(), module)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.render(info)
		},
	}
}

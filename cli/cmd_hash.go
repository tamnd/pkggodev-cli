package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) hashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hash <module@version>",
		Short: "Look up the checksum database entry for a module version",
		Long: `Look up the hash record for a specific module version from sum.golang.org.

Returns the tree hash (h1:...) and go.mod hash (h1:...) that go tools use to
verify downloads. The argument must be in module@version form.`,
		Args:    cobra.ExactArgs(1),
		Example: "  gopkg hash github.com/spf13/cobra@v1.8.0\n  gopkg hash golang.org/x/net@v0.20.0 -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleAt := args[0]
			a.progressf("looking up hash for %s...", moduleAt)
			info, err := a.client.Hash(cmd.Context(), moduleAt)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.render(info)
		},
	}
}

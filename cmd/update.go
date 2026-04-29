package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	selfupdate "github.com/wow-look-at-my/go-selfupdate-mini"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the binary to the latest version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo := selfupdate.NewRepositorySlug("wow-look-at-my", "compose-remote")
		ver := currentVersion()
		rel, err := selfupdate.UpdateSelf(cmd.Context(), ver, repo)
		if err != nil {
			return err
		}
		if rel.Version.Version == ver {
			fmt.Fprintln(cmd.OutOrStdout(), "Already up-to-date.")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Updated to %s\n", rel.Version.Version)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(updateCmd) }

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "compose-remote",
	Short: "Reconcile a docker compose stack against a remote source",
	Long: `compose-remote watches a docker-compose.yml from a file, HTTP URL, or git
repo and continuously reconciles the running containers on this host against
the desired state expressed by that file. It works around docker compose's
"up-to-date" bug by force-recreating any service compose wrongly skips.`,
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

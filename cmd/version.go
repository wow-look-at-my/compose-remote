package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	selfupdate "github.com/wow-look-at-my/go-selfupdate-mini"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("compose-remote", currentVersion())
	},
}

func init() { rootCmd.AddCommand(versionCmd) }

func currentVersion() string { return selfupdate.CurrentVersion() }

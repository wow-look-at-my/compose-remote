package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(_ *cobra.Command, _ []string) {
		v := "(devel)"
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" {
				v = info.Main.Version
			}
		}
		fmt.Println("compose-remote", v)
	},
}

func init() { rootCmd.AddCommand(versionCmd) }

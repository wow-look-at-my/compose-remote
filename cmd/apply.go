package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/wow-look-at-my/compose-remote/internal/runner"
	"github.com/wow-look-at-my/compose-remote/internal/secrets"
	"github.com/wow-look-at-my/compose-remote/internal/source"
	"github.com/wow-look-at-my/compose-remote/internal/state"
)

var applyFlags struct {
	name     string
	project  string
	stateDir string

	sopsEnvFiles []string

	ensureNetworks     bool
	ensureBindMounts   bool
	ensureNamedVolumes bool

	source source.Flags
}

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Perform one reconcile pass and exit",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if applyFlags.name == "" {
			return fmt.Errorf("--name is required")
		}
		if applyFlags.project == "" {
			applyFlags.project = applyFlags.name
		}
		if applyFlags.stateDir == "" {
			applyFlags.stateDir = defaultStateDir()
		}
		if err := secrets.LoadEnv(cmd.Context(), secrets.SopsCLI, applyFlags.sopsEnvFiles); err != nil {
			return fmt.Errorf("load sops env: %w", err)
		}
		dir, err := state.New(applyFlags.stateDir, applyFlags.name)
		if err != nil {
			return err
		}
		applyFlags.source.StateDir = dir.Path()
		src, err := source.New(applyFlags.source)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return runner.RunOnce(ctx, runner.Config{
			Source:             src,
			State:              dir,
			Project:            applyFlags.project,
			EnsureNetworks:     applyFlags.ensureNetworks,
			EnsureBindMounts:   applyFlags.ensureBindMounts,
			EnsureNamedVolumes: applyFlags.ensureNamedVolumes,
		})
	},
}

func init() {
	applyCmd.Flags().StringVar(&applyFlags.name, "name", "", "stack name (required)")
	applyCmd.Flags().StringVar(&applyFlags.project, "project", "", "docker compose project name (default: --name)")
	applyCmd.Flags().StringVar(&applyFlags.stateDir, "state-dir", "", "state directory (default: $XDG_STATE_HOME/compose-remote)")
	applyCmd.Flags().StringSliceVar(&applyFlags.sopsEnvFiles, "sops-env-file", nil, "path to a sops-encrypted dotenv file; decrypted values are exported into the process and become available for ${VAR} substitution in the compose file. Repeatable.")
	applyCmd.Flags().BoolVar(&applyFlags.ensureNetworks, "ensure-networks", true, "pre-create any external: true networks the compose file references that don't already exist")
	applyCmd.Flags().BoolVar(&applyFlags.ensureBindMounts, "ensure-bind-mounts", true, "mkdir -p any absolute bind-mount host paths that don't already exist")
	applyCmd.Flags().BoolVar(&applyFlags.ensureNamedVolumes, "ensure-named-volumes", true, "pre-create any external: true named volumes that don't already exist")
	addSourceFlags(applyCmd, &applyFlags.source)
	rootCmd.AddCommand(applyCmd)
}

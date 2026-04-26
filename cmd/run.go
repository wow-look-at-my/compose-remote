package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	selfupdate "github.com/wow-look-at-my/go-selfupdate-mini"

	"github.com/wow-look-at-my/compose-remote/internal/log"
	"github.com/wow-look-at-my/compose-remote/internal/runner"
	"github.com/wow-look-at-my/compose-remote/internal/source"
	"github.com/wow-look-at-my/compose-remote/internal/state"
)

var runFlags struct {
	name         string
	project      string
	stateDir     string
	interval     time.Duration
	pullInterval time.Duration
	once         bool

	autoUpdate         bool
	autoUpdateInterval time.Duration

	source source.Flags
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the reconcile loop until interrupted",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if runFlags.name == "" {
			return fmt.Errorf("--name is required")
		}
		if runFlags.project == "" {
			runFlags.project = runFlags.name
		}
		if runFlags.stateDir == "" {
			runFlags.stateDir = defaultStateDir()
		}

		dir, err := state.New(runFlags.stateDir, runFlags.name)
		if err != nil {
			return err
		}
		runFlags.source.StateDir = dir.Path()
		src, err := source.New(runFlags.source)
		if err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if runFlags.autoUpdate && !runFlags.once {
			go autoUpdateLoop(ctx, currentVersion(), runFlags.autoUpdateInterval)
		}

		cfg := runner.Config{
			Source:       src,
			State:        dir,
			Project:      runFlags.project,
			Interval:     runFlags.interval,
			PullInterval: runFlags.pullInterval,
		}
		if runFlags.once {
			return runner.RunOnce(ctx, cfg)
		}
		return runner.Run(ctx, cfg)
	},
}

func init() {
	runCmd.Flags().StringVar(&runFlags.name, "name", "", "stack name (required)")
	runCmd.Flags().StringVar(&runFlags.project, "project", "", "docker compose project name (default: --name)")
	runCmd.Flags().StringVar(&runFlags.stateDir, "state-dir", "", "state directory (default: $XDG_STATE_HOME/compose-remote)")
	runCmd.Flags().DurationVar(&runFlags.interval, "interval", 30*time.Second, "reconcile interval")
	runCmd.Flags().DurationVar(&runFlags.pullInterval, "pull-interval", 0, "if > 0, run `docker compose pull` for all services on this cadence; the next reconcile then recreates any container whose image SHA drifted (default: disabled)")
	runCmd.Flags().BoolVar(&runFlags.once, "once", false, "perform a single reconcile pass and exit")
	runCmd.Flags().BoolVar(&runFlags.autoUpdate, "auto-update", true, "periodically check for a newer release and replace the binary (requires pm2 or similar to restart)")
	runCmd.Flags().DurationVar(&runFlags.autoUpdateInterval, "auto-update-interval", time.Hour, "how often to check for updates")

	addSourceFlags(runCmd, &runFlags.source)
	rootCmd.AddCommand(runCmd)
}

func addSourceFlags(cmd *cobra.Command, f *source.Flags) {
	cmd.Flags().StringVar(&f.File, "file", "", "path to a local docker-compose.yml")
	cmd.Flags().StringVar(&f.URL, "url", "", "http(s) URL to a docker-compose.yml")
	cmd.Flags().StringVar(&f.Git, "git", "", "git repo URL hosting the docker-compose.yml")
	cmd.Flags().StringVar(&f.GitRef, "git-ref", "", "git branch, tag, or commit (default: repo HEAD)")
	cmd.Flags().StringVar(&f.GitPath, "git-path", "docker-compose.yml", "path of the compose file inside the git repo")
	cmd.Flags().StringVar(&f.GitSSH, "git-ssh-key", "", "path to an SSH private key for the git source")
}

func autoUpdateLoop(ctx context.Context, ver string, interval time.Duration) {
	if ver == "(devel)" {
		log.Warn("auto-update skipped: running a development build")
		return
	}

	repo := selfupdate.NewRepositorySlug("wow-look-at-my", "compose-remote")

	tryUpdate := func() {
		rel, err := selfupdate.UpdateSelf(ctx, ver, repo)
		if err != nil {
			log.Warn("auto-update failed", log.KV{K: "err", V: err.Error()})
			return
		}
		if rel.Version.Version != ver {
			log.Info("auto-update applied, restarting",
				log.KV{K: "from", V: ver},
				log.KV{K: "to", V: rel.Version.Version},
			)
			os.Exit(0)
		}
	}

	tryUpdate()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tryUpdate()
		}
	}
}

func defaultStateDir() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v + "/compose-remote"
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h + "/.local/state/compose-remote"
	}
	return "./.compose-remote-state"
}

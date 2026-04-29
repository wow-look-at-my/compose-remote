package cmd

import (
	selfupdate "github.com/wow-look-at-my/go-selfupdate-mini"
)

func init() {
	repo := selfupdate.NewRepositorySlug("wow-look-at-my", "compose-remote")
	selfupdate.RegisterCommands(rootCmd, repo)
}

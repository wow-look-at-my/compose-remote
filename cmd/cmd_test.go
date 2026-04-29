package cmd

import (
	"strings"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
)

func TestDefaultStateDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	got := defaultStateDir()
	assert.Equal(t, "/tmp/xdg/compose-remote", got)

}

func TestDefaultStateDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	got := defaultStateDir()
	assert.False(t, !strings.HasSuffix(got, "/.local/state/compose-remote") && got != "./.compose-remote-state")

}

func TestRootHasSubcommands(t *testing.T) {
	for _, want := range []string{"run", "apply", "version"} {
		_, _, err := rootCmd.Find([]string{want})
		assert.Nil(t, err)

	}
}

func TestRunRequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"run", "--file", "x"})
	t.Cleanup(func() { rootCmd.SetArgs(nil); runFlags.name = "" })
	err := rootCmd.Execute()
	assert.False(t, err == nil || !strings.Contains(err.Error(), "name"))

}

func TestApplyRequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"apply", "--file", "x"})
	t.Cleanup(func() { rootCmd.SetArgs(nil); applyFlags.name = "" })
	err := rootCmd.Execute()
	assert.False(t, err == nil || !strings.Contains(err.Error(), "name"))
}

func TestVersionRunsCleanly(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.Nil(t, rootCmd.Execute())
}

func TestExecuteUsesRootCommand(t *testing.T) {
	// Execute() reads from os.Args via rootCmd. Force it to a known-good
	// invocation (version) so it returns nil and exercises the wrapper.
	rootCmd.SetArgs([]string{"version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	Execute()
}

func TestReleaseVersionRe(t *testing.T) {
	// The auto-update gate. Real tagged releases opt in; pseudo-versions,
	// dirty builds, and dev fallbacks opt out. This is the regression test
	// for the "v0.0.X != 0.0.X" pm2 restart loop.
	wantUpdate := []string{"0.0.1777263819", "1.2.3", "10.20.30"}
	skipUpdate := []string{
		"v0.0.1777263819",                           // raw build info, not normalized
		"0.0.0-20260427042339-a358050861c7+dirty",   // Go pseudo-version
		"1.2.3-beta",                                // semver pre-release
		"1.2.3+meta",                                // semver build metadata
		"(devel)",                                   // no info at all
		"a358050861c7",                              // short VCS revision
		"a358050861c7+dirty",                        // dirty VCS revision
		"",
	}
	for _, v := range wantUpdate {
		assert.True(t, releaseVersionRe.MatchString(v))
	}
	for _, v := range skipUpdate {
		assert.False(t, releaseVersionRe.MatchString(v))
	}
}

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

package cmd

import (
	"strings"
	"testing"
)

func TestDefaultStateDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	got := defaultStateDir()
	if got != "/tmp/xdg/compose-remote" {
		t.Errorf("defaultStateDir = %q", got)
	}
}

func TestDefaultStateDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	got := defaultStateDir()
	if !strings.HasSuffix(got, "/.local/state/compose-remote") &&
		got != "./.compose-remote-state" {
		t.Errorf("unexpected default state dir: %q", got)
	}
}

func TestRootHasSubcommands(t *testing.T) {
	for _, want := range []string{"run", "apply", "version"} {
		_, _, err := rootCmd.Find([]string{want})
		if err != nil {
			t.Errorf("missing subcommand %q: %v", want, err)
		}
	}
}

func TestRunRequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"run", "--file", "x"})
	t.Cleanup(func() { rootCmd.SetArgs(nil); runFlags.name = "" })
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("expected --name required error, got %v", err)
	}
}

func TestApplyRequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"apply", "--file", "x"})
	t.Cleanup(func() { rootCmd.SetArgs(nil); applyFlags.name = "" })
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("expected --name required error, got %v", err)
	}
}

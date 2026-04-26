package secrets

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestParseEnv(t *testing.T) {
	in := `# comment
KEY1=value1
KEY2 = value2
KEY3="quoted value"
KEY4='single quoted'
KEY5=

# trailing comment
`
	got, err := parseEnv(in)
	require.NoError(t, err)
	assert.Equal(t, "value1", got["KEY1"])
	assert.Equal(t, "value2", got["KEY2"])
	assert.Equal(t, "quoted value", got["KEY3"])
	assert.Equal(t, "single quoted", got["KEY4"])
	assert.Equal(t, "", got["KEY5"])
}

func TestParseEnvMissingEquals(t *testing.T) {
	_, err := parseEnv("BARE_LINE\n")
	assert.NotNil(t, err)
}

func TestParseEnvEmptyKey(t *testing.T) {
	_, err := parseEnv("=value\n")
	assert.NotNil(t, err)
}

func TestParseEnvBlankAndComments(t *testing.T) {
	got, err := parseEnv("\n\n#hello\n  # indented comment\nA=1\n")
	require.NoError(t, err)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "1", got["A"])
}

func TestLoadEnvAppliesValues(t *testing.T) {
	t.Setenv("LOAD_ENV_TEST_KEY", "")
	dec := func(_ context.Context, _ string) (string, error) {
		return "LOAD_ENV_TEST_KEY=hello\n", nil
	}
	require.NoError(t, LoadEnv(context.Background(), dec, []string{"a"}))
	assert.Equal(t, "hello", os.Getenv("LOAD_ENV_TEST_KEY"))
}

func TestLoadEnvLaterFileWins(t *testing.T) {
	t.Setenv("OVERRIDE_KEY", "")
	calls := 0
	dec := func(_ context.Context, _ string) (string, error) {
		calls++
		if calls == 1 {
			return "OVERRIDE_KEY=first\n", nil
		}
		return "OVERRIDE_KEY=second\n", nil
	}
	require.NoError(t, LoadEnv(context.Background(), dec, []string{"a", "b"}))
	assert.Equal(t, "second", os.Getenv("OVERRIDE_KEY"))
}

func TestLoadEnvDecryptError(t *testing.T) {
	dec := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("decrypt boom")
	}
	err := LoadEnv(context.Background(), dec, []string{"a"})
	assert.NotNil(t, err)
}

func TestLoadEnvParseError(t *testing.T) {
	dec := func(_ context.Context, _ string) (string, error) {
		return "BARE\n", nil
	}
	err := LoadEnv(context.Background(), dec, []string{"a"})
	assert.NotNil(t, err)
}

func TestSopsCLIMissingFile(t *testing.T) {
	// SopsCLI shells out; if `sops` is on PATH, the missing-file error
	// surfaces from sops itself. If sops isn't installed, exec.LookPath
	// fails. Either way we expect a non-nil error.
	_, err := SopsCLI(context.Background(), "/no/such/file.env")
	assert.NotNil(t, err)
}

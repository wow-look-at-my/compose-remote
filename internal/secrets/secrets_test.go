package secrets

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestParseDotenv(t *testing.T) {
	in := `# comment
KEY1=value1
KEY2 = value2
KEY3="quoted value"
KEY4='single quoted'
KEY5=

# trailing comment
`
	got, err := parseDotenv(in)
	require.NoError(t, err)
	assert.Equal(t, "value1", got["KEY1"])
	assert.Equal(t, "value2", got["KEY2"])
	assert.Equal(t, "quoted value", got["KEY3"])
	assert.Equal(t, "single quoted", got["KEY4"])
	assert.Equal(t, "", got["KEY5"])
}

func TestParseDotenvMissingEquals(t *testing.T) {
	_, err := parseDotenv("BARE_LINE\n")
	assert.NotNil(t, err)
}

func TestParseDotenvEmptyKey(t *testing.T) {
	_, err := parseDotenv("=value\n")
	assert.NotNil(t, err)
}

func TestParseDotenvBlankAndComments(t *testing.T) {
	got, err := parseDotenv("\n\n#hello\n  # indented comment\nA=1\n")
	require.NoError(t, err)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "1", got["A"])
}

func TestParseJSON(t *testing.T) {
	in := `{
  "API_KEY": "abc123",
  "DEBUG": true,
  "REPLICAS": 3,
  "BANNER": null,
  "EMPTY": ""
}`
	got, err := parseJSON(in)
	require.NoError(t, err)
	assert.Equal(t, "abc123", got["API_KEY"])
	assert.Equal(t, "true", got["DEBUG"])
	assert.Equal(t, "3", got["REPLICAS"])
	assert.Equal(t, "", got["BANNER"])
	assert.Equal(t, "", got["EMPTY"])
}

func TestParseJSONRejectsNested(t *testing.T) {
	_, err := parseJSON(`{"db": {"pass": "x"}}`)
	assert.NotNil(t, err)
}

func TestParseJSONRejectsArrays(t *testing.T) {
	_, err := parseJSON(`{"hosts": ["a", "b"]}`)
	assert.NotNil(t, err)
}

func TestParseJSONRejectsFractional(t *testing.T) {
	_, err := parseJSON(`{"ratio": 1.5}`)
	assert.NotNil(t, err)
}

func TestParseJSONInvalid(t *testing.T) {
	_, err := parseJSON(`{not valid`)
	assert.NotNil(t, err)
}

func TestParseYAML(t *testing.T) {
	in := `API_KEY: abc123
DEBUG: true
REPLICAS: 3
BANNER: ~
EMPTY: ""
`
	got, err := parseYAML(in)
	require.NoError(t, err)
	assert.Equal(t, "abc123", got["API_KEY"])
	assert.Equal(t, "true", got["DEBUG"])
	assert.Equal(t, "3", got["REPLICAS"])
	assert.Equal(t, "", got["BANNER"])
	assert.Equal(t, "", got["EMPTY"])
}

func TestParseYAMLRejectsNested(t *testing.T) {
	_, err := parseYAML("db:\n  pass: x\n")
	assert.NotNil(t, err)
}

func TestParseYAMLInvalid(t *testing.T) {
	_, err := parseYAML("key: : value\n")
	assert.NotNil(t, err)
}

func TestParseDispatchByExtension(t *testing.T) {
	cases := []struct {
		path    string
		content string
		key     string
		want    string
	}{
		{"a.json", `{"K":"v"}`, "K", "v"},
		{"a.yaml", "K: v\n", "K", "v"},
		{"a.yml", "K: v\n", "K", "v"},
		{"a.env", "K=v\n", "K", "v"},
	}
	for _, c := range cases {
		got, err := parse(c.path, c.content)
		require.NoError(t, err, c.path)
		assert.Equal(t, c.want, got[c.key])
	}
}

func TestParseRejectsUnknownExtension(t *testing.T) {
	_, err := parse("secrets.toml", "K = \"v\"\n")
	assert.NotNil(t, err)
}

func TestParseRejectsNoExtension(t *testing.T) {
	_, err := parse("secrets", "K=v\n")
	assert.NotNil(t, err)
}

func TestLoadEnvAppliesValues(t *testing.T) {
	t.Setenv("LOAD_ENV_TEST_KEY", "")
	dec := func(_ context.Context, _ string) (string, error) {
		return `{"LOAD_ENV_TEST_KEY":"hello"}`, nil
	}
	require.NoError(t, LoadEnv(context.Background(), dec, []string{"a.json"}))
	assert.Equal(t, "hello", os.Getenv("LOAD_ENV_TEST_KEY"))
}

func TestLoadEnvLaterFileWins(t *testing.T) {
	t.Setenv("OVERRIDE_KEY", "")
	calls := 0
	dec := func(_ context.Context, _ string) (string, error) {
		calls++
		if calls == 1 {
			return `{"OVERRIDE_KEY":"first"}`, nil
		}
		return `{"OVERRIDE_KEY":"second"}`, nil
	}
	require.NoError(t, LoadEnv(context.Background(), dec, []string{"a.json", "b.json"}))
	assert.Equal(t, "second", os.Getenv("OVERRIDE_KEY"))
}

func TestLoadEnvDecryptError(t *testing.T) {
	dec := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("decrypt boom")
	}
	err := LoadEnv(context.Background(), dec, []string{"a.json"})
	assert.NotNil(t, err)
}

func TestLoadEnvParseError(t *testing.T) {
	dec := func(_ context.Context, _ string) (string, error) {
		return `{not valid json`, nil
	}
	err := LoadEnv(context.Background(), dec, []string{"a.json"})
	assert.NotNil(t, err)
}

func TestLoadEnvUnknownExtension(t *testing.T) {
	dec := func(_ context.Context, _ string) (string, error) {
		return "K=v\n", nil
	}
	err := LoadEnv(context.Background(), dec, []string{"a.toml"})
	assert.NotNil(t, err)
}

func TestSopsCLIMissingFile(t *testing.T) {
	// SopsCLI shells out; if `sops` is on PATH, the missing-file error
	// surfaces from sops itself. If sops isn't installed, exec.LookPath
	// fails. Either way we expect a non-nil error.
	_, err := SopsCLI(context.Background(), "/no/such/file.env")
	assert.NotNil(t, err)
}

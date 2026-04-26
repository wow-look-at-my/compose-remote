// Package secrets loads sops-encrypted secrets files into the process
// environment so that ${VAR} substitutions inside the compose YAML pick
// up the decrypted values.
//
// Three storage formats are supported, distinguished by file extension:
//
//   - .json — top-level JSON object (recommended; unambiguous parsing)
//   - .yaml / .yml — top-level YAML mapping
//   - .env — dotenv-style KEY=VALUE lines
//
// Any other extension is rejected. Nested values (objects, arrays) inside
// a JSON or YAML file are rejected as well: env vars are flat strings and
// silently flattening a structured file would be ambiguous.
//
// Decryption shells out to the `sops` binary; we deliberately do not
// import sops as a Go library because its dep tree (cloud KMS clients,
// etc.) dwarfs the rest of compose-remote.
package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Decrypter takes the path to a sops-encrypted file and returns its
// plaintext contents. Swappable for tests.
type Decrypter func(ctx context.Context, path string) (string, error)

// SopsCLI runs `sops decrypt <path>` and returns stdout. This is the
// default Decrypter used in production. sops auto-detects the file
// format from the extension; we do not pass --input-type.
func SopsCLI(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "sops", "decrypt", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sops decrypt %s: %w: %s",
			path, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// LoadEnv decrypts each path via dec and sets the resulting KEY=VALUE
// pairs into the process environment with os.Setenv. The plaintext is
// never written to disk.
//
// File format is determined by extension (.json, .yaml/.yml, .env).
// Files are processed in order; if two files set the same key, the later
// one wins (matching shell `source` semantics).
//
// On error, the partial state may have already set some variables. The
// caller should treat any error as fatal and not start the daemon.
func LoadEnv(ctx context.Context, dec Decrypter, paths []string) error {
	for _, p := range paths {
		content, err := dec(ctx, p)
		if err != nil {
			return err
		}
		kv, err := parse(p, content)
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		for k, v := range kv {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("setenv %s: %w", k, err)
			}
		}
	}
	return nil
}

// parse picks the right parser based on the file extension.
func parse(path, content string) (map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return parseJSON(content)
	case ".yaml", ".yml":
		return parseYAML(content)
	case ".env":
		return parseDotenv(content)
	default:
		return nil, fmt.Errorf(
			"unsupported extension on %q: expected .json, .yaml, .yml, or .env",
			filepath.Base(path))
	}
}

// parseJSON parses a top-level JSON object as flat KEY=VALUE pairs.
// Nested objects/arrays are an error.
func parseJSON(content string) (map[string]string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return flatten(raw)
}

// parseYAML parses a top-level YAML mapping as flat KEY=VALUE pairs.
// Nested mappings/sequences are an error.
func parseYAML(content string) (map[string]string, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	return flatten(raw)
}

// flatten converts a top-level scalar map into stringified env values.
// Strings pass through as-is. Booleans become "true"/"false". Integers
// and integer-valued floats become decimal strings. nulls become "".
// Anything else (nested objects, arrays, fractional numbers) is rejected
// rather than silently coerced — env vars are unstructured strings, and
// silently squashing structure here would surprise callers.
func flatten(raw map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			out[k] = val
		case bool:
			out[k] = strconv.FormatBool(val)
		case int:
			out[k] = strconv.Itoa(val)
		case int64:
			out[k] = strconv.FormatInt(val, 10)
		case float64:
			if val == float64(int64(val)) {
				out[k] = strconv.FormatInt(int64(val), 10)
			} else {
				return nil, fmt.Errorf("key %q: fractional numbers are not allowed in env values", k)
			}
		case nil:
			out[k] = ""
		default:
			return nil, fmt.Errorf("key %q: nested values are not allowed (got %T)", k, v)
		}
	}
	return out, nil
}

// parseDotenv parses dotenv-style content. Each non-blank, non-comment
// line is "KEY=VALUE". Surrounding single or double quotes on the value
// are stripped. We do NOT perform shell expansion or `${...}` substitution.
func parseDotenv(content string) (map[string]string, error) {
	out := map[string]string{}
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: missing '='", i+1)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", i+1)
		}
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 {
			f, l := value[0], value[len(value)-1]
			if (f == '"' && l == '"') || (f == '\'' && l == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[key] = value
	}
	return out, nil
}

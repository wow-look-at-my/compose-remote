// Package secrets loads sops-encrypted dotenv files into the process
// environment so that ${VAR} substitutions inside the compose YAML pick
// up the decrypted values.
//
// Decryption shells out to the `sops` binary; we deliberately do not
// import sops as a Go library because its dep tree (cloud KMS clients,
// etc.) dwarfs the rest of compose-remote.
package secrets

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Decrypter takes the path to a sops-encrypted file and returns its
// plaintext contents. Swappable for tests.
type Decrypter func(ctx context.Context, path string) (string, error)

// SopsCLI runs `sops decrypt <path>` and returns stdout. This is the
// default Decrypter used in production.
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
		kv, err := parseEnv(content)
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

// parseEnv parses dotenv-style content. Each non-blank, non-comment line
// is "KEY=VALUE". Surrounding single or double quotes on the value are
// stripped. We do NOT perform shell expansion or `${...}` substitution.
func parseEnv(content string) (map[string]string, error) {
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

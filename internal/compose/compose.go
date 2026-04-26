package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/log"
)

// runner is the unit of "shell out to a binary" we depend on. Swappable
// for tests via Client.runner = fakeRunner{...}.
type runner interface {
	// composeArgs runs `docker compose -f <file> -p <project> <args...>`.
	composeArgs(ctx context.Context, file, project string, args ...string) (string, error)
	// inspect runs `docker inspect <id>`.
	inspect(ctx context.Context, id string) (string, error)
	// imageInspect runs `docker image inspect --format={{.Id}} <image>`.
	imageInspect(ctx context.Context, image string) (string, error)
	// version runs `docker compose version` (used by EnsureAvailable).
	version(ctx context.Context) (string, error)
}

// Client wraps the `docker compose` v2 CLI for one project.
type Client struct {
	// File is the path passed to `docker compose -f`.
	File string
	// Project is the project name passed to `docker compose -p`.
	Project string

	r runner
}

// New constructs a Client that shells out to the real docker binary.
func New(file, project string) *Client {
	return &Client{File: file, Project: project, r: realRunner{}}
}

// EnsureAvailable returns an error if `docker compose version` does not
// run cleanly. Call once at startup.
func EnsureAvailable(ctx context.Context) error {
	if _, err := (realRunner{}).version(ctx); err != nil {
		return fmt.Errorf("`docker compose version` failed: %w", err)
	}
	return nil
}

// Container is a parsed entry from `docker compose ps --format json`.
type Container struct {
	ID         string    `json:"ID"`
	Name       string    `json:"Name"`
	Service    string    `json:"Service"`
	Image      string    `json:"Image"`
	State      string    `json:"State"`
	Health     string    `json:"Health"`
	ExitCode   int       `json:"ExitCode"`
	CreatedAt  time.Time `json:"-"`
	ConfigHash string    `json:"-"`
	ImageID    string    `json:"-"` // sha256 of the image the container was created from
}

// Ps returns one Container per running compose-managed container.
// State is enriched via `docker inspect` to fill CreatedAt, ConfigHash, ImageID.
func (c *Client) Ps(ctx context.Context) ([]Container, error) {
	out, err := c.r.composeArgs(ctx, c.File, c.Project, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	containers, err := parsePs(out)
	if err != nil {
		return nil, err
	}
	for i := range containers {
		if err := c.enrich(ctx, &containers[i]); err != nil {
			return nil, fmt.Errorf("inspect %s: %w", containers[i].ID, err)
		}
	}
	return containers, nil
}

// `docker compose ps --format json` emits either a JSON array OR newline-
// delimited JSON depending on compose version. Handle both.
func parsePs(out string) ([]Container, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	if strings.HasPrefix(out, "[") {
		var arr []Container
		if err := json.Unmarshal([]byte(out), &arr); err != nil {
			return nil, fmt.Errorf("parse compose ps json array: %w", err)
		}
		return arr, nil
	}
	var result []Container
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item Container
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("parse compose ps json line: %w", err)
		}
		result = append(result, item)
	}
	return result, nil
}

// enrich fills CreatedAt, ConfigHash, ImageID from `docker inspect`.
func (c *Client) enrich(ctx context.Context, ct *Container) error {
	type inspectResult struct {
		Created string `json:"Created"`
		Image   string `json:"Image"`
		Config  struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	out, err := c.r.inspect(ctx, ct.ID)
	if err != nil {
		return err
	}
	var arr []inspectResult
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		return fmt.Errorf("parse docker inspect: %w", err)
	}
	if len(arr) == 0 {
		return fmt.Errorf("docker inspect returned no entries for %s", ct.ID)
	}
	t, err := time.Parse(time.RFC3339Nano, arr[0].Created)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, arr[0].Created)
	}
	ct.CreatedAt = t
	ct.ImageID = arr[0].Image
	ct.ConfigHash = arr[0].Config.Labels["com.docker.compose.config-hash"]
	return nil
}

// Pull pulls the listed services. If services is empty, pulls all.
func (c *Client) Pull(ctx context.Context, services ...string) error {
	args := []string{"pull"}
	args = append(args, services...)
	_, err := c.r.composeArgs(ctx, c.File, c.Project, args...)
	return err
}

// ImageID returns the SHA digest of the locally cached image with the
// given reference (e.g. "traefik:v3"). Returns an empty string and a
// nil error if the image is not present locally -- callers should treat
// that as "no drift detectable" rather than as a failure, since periodic
// pulls may not have populated the cache yet.
func (c *Client) ImageID(ctx context.Context, image string) (string, error) {
	out, err := c.r.imageInspect(ctx, image)
	if err != nil {
		if isImageNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// isImageNotFound matches the stderr substring docker emits when
// `image inspect` is asked about a tag that hasn't been pulled. We
// intentionally don't try to parse exit codes here: the message format
// is stable enough across recent docker versions and the cost of a
// false negative is minor (we'd just refuse to detect SHA drift on
// that one image).
func isImageNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "No such image") || strings.Contains(s, "no such image")
}

// Up runs `up -d --remove-orphans --wait`. The --wait is non-negotiable:
// without it the bug-fix pass races against unstarted containers.
func (c *Client) Up(ctx context.Context) error {
	_, err := c.r.composeArgs(ctx, c.File, c.Project, "up", "-d", "--remove-orphans", "--wait")
	return err
}

// ForceRecreate runs `up -d --force-recreate --no-deps --wait <service>`.
// Same --wait mandate as Up.
func (c *Client) ForceRecreate(ctx context.Context, service string) error {
	_, err := c.r.composeArgs(ctx, c.File, c.Project,
		"up", "-d", "--force-recreate", "--no-deps", "--wait", service)
	return err
}

// realRunner shells out to the actual docker binary.
type realRunner struct{}

func (realRunner) composeArgs(ctx context.Context, file, project string, args ...string) (string, error) {
	full := append([]string{"compose", "-f", file, "-p", project}, args...)
	return runDocker(ctx, full...)
}

func (realRunner) inspect(ctx context.Context, id string) (string, error) {
	return runDocker(ctx, "inspect", id)
}

func (realRunner) imageInspect(ctx context.Context, image string) (string, error) {
	return runDocker(ctx, "image", "inspect", "--format={{.Id}}", image)
}

func (realRunner) version(ctx context.Context) (string, error) {
	return runDocker(ctx, "compose", "version")
}

func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Debug("docker failed",
			log.KV{K: "args", V: strings.Join(args, " ")},
			log.KV{K: "stderr", V: strings.TrimSpace(stderr.String())},
		)
		return stdout.String(), fmt.Errorf("docker %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

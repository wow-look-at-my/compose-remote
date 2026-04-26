# compose-remote

A small Go daemon that watches a `docker-compose.yml` (from a local file,
HTTP URL, or git repo) and continuously reconciles the running containers
on the host against the desired state expressed by that file.

It exists because Docker Compose has a long-standing family of bugs where
a config change in the YAML (env_file content, bind-mount source, label,
healthcheck, etc.) is correctly written to the file but
`docker compose up -d` reports the container "is up-to-date" and skips
recreation. compose-remote always invokes
`docker compose up -d --remove-orphans --wait` and then runs a bug-fix
pass that force-recreates any service compose wrongly skipped.

It also makes the model declarative: rather than detecting *changes* (an
event), it reconciles *differences* (a state). If a container was
`docker rm`-ed behind its back, or if the host was offline when the YAML
changed, the next reconcile tick still puts the host back into the desired
state.

One process == one stack. Run several instances under pm2 (or any other
process supervisor) for several stacks.

## Build

```sh
go-toolchain
```

The resulting binary is `./compose-remote`.

## Usage

```sh
compose-remote run --name my-stack --file ./compose.yml
compose-remote run --name my-stack --url https://example.com/compose.yml
compose-remote run --name my-stack \
    --git https://github.com/me/infra.git \
    --git-ref main \
    --git-path stacks/my-stack/docker-compose.yml
```

Common flags:

| Flag                        | Purpose                                                                        |
|-----------------------------|--------------------------------------------------------------------------------|
| `--name`                    | Stack name. Required. Used as the docker compose project name by default.     |
| `--project`                 | Override the docker compose project name.                                      |
| `--state-dir`               | Where compose-remote keeps its working files (default: `$XDG_STATE_HOME/compose-remote` or `~/.local/state/compose-remote`). |
| `--interval`                | How often to re-check (default `30s`).                                         |
| `--once`                    | Reconcile once and exit (handy for tests / cron).                              |
| `--auto-update`             | Enable background self-update checks (requires a process supervisor to restart). |
| `--auto-update-interval`    | How often to poll for a new release (default `1h`).                            |
| `--sops-env-file`           | Path to a sops-encrypted secrets file (`.json`, `.yaml`/`.yml`, or `.env`). Repeatable. Decrypted at startup; values are exported into the daemon process so `${VAR}` substitutions in the compose YAML resolve. Plaintext is never written to disk. |

Source flags (mutually exclusive, exactly one required):

| Flag             | Purpose                                                            |
|------------------|--------------------------------------------------------------------|
| `--file`         | Path to a local docker-compose.yml.                                |
| `--url`          | http(s) URL serving a docker-compose.yml. ETag-cached.             |
| `--git`          | Git repo URL.                                                      |
| `--git-ref`      | Branch, tag, or commit SHA. Default: HEAD of the default branch.   |
| `--git-path`     | Path inside the repo. Default: `docker-compose.yml`.               |
| `--git-ssh-key`  | SSH private key path for SSH-based git auth.                       |

The one-shot variant:

```sh
compose-remote apply --name my-stack --file ./compose.yml
```

This runs a single reconcile pass and exits.

## Secrets (sops)

Compose files often reference secrets via `${VAR}` substitution. Rather
than committing those values in plaintext or babysitting an external
wrapper that decrypts a file before launching compose-remote, the daemon
can read sops-encrypted secrets files directly:

```sh
compose-remote run \
    --name web-stack \
    --file ./compose.yml \
    --sops-env-file ./secrets/web-stack.json
```

Three storage formats are supported, distinguished by file extension:

| Extension          | Format                                                         |
|--------------------|----------------------------------------------------------------|
| `.json`            | Top-level JSON object. Recommended — unambiguous quoting.      |
| `.yaml` / `.yml`   | Top-level YAML mapping.                                        |
| `.env`             | Dotenv-style `KEY=VALUE` lines.                                |

Any other extension is rejected. Within a JSON or YAML file, only
top-level scalar values are allowed (string, bool, integer, null);
nested objects/arrays/fractional numbers are rejected, because env vars
are flat strings and silently flattening structure would surprise
callers.

A `secrets.json` example:

```json
{
  "DATABASE_URL": "postgres://prod-db/foo",
  "API_KEY": "abc123",
  "DEBUG": false
}
```

After encrypting in place with `sops -e -i secrets.json`, the keys stay
readable and the values are encrypted. compose-remote runs `sops decrypt`
at startup, parses the result, and exports each pair into its own
process environment. Docker compose, invoked as a child, inherits those
vars and expands `${DATABASE_URL}` etc. in the YAML. Plaintext never
touches disk.

`--sops-env-file` may be repeated. Files are processed in order; later
files override earlier ones, matching shell `source` semantics.

To rotate a secret, update the encrypted file and restart compose-remote
(e.g. `pm2 restart <stack>`). The next reconcile picks up the new value.

## How it works

Every `--interval`:

1. Fetch the compose file from the source. HTTP uses ETag/Last-Modified
   to short-circuit unchanged content. Git does a shallow `git fetch` +
   `git reset --hard <ref>`. File reads the file directly.
2. Parse the file, compute a deterministic per-service config hash
   (including any referenced `env_file:` content), and inject the hash as
   a label `io.compose-remote.config-hash` on each service.
3. Inspect every running compose-managed container on the host and read
   its label.
4. Categorize each desired service as: `missing`, `drifted-config`,
   `drifted-image`, `unhealthy`, or in-sync.
5. If any service is non-sync:
   - `docker compose pull <svc>` for image-drifted services only (no
     blanket pulls — the user explicitly opted out of waste).
   - `docker compose up -d --remove-orphans --wait`. The `--wait` is
     mandatory.
   - **Bug-fix pass**: re-inspect; for each service still using the
     pre-apply container ID (i.e. compose returned "up-to-date" when it
     shouldn't have), run
     `docker compose up -d --force-recreate --no-deps --wait <svc>`.

Pulls only happen when a service's image string changed in the YAML. The
tool will not chase floating tags (`:latest`) on its own — pin a digest
or change the YAML.

## Self-update

compose-remote can update itself from GitHub releases.

One-shot manual update:

```sh
compose-remote update
```

To enable automatic background updates while the daemon is running, add
`--auto-update` to the `run` invocation:

```sh
compose-remote run --name my-stack --file ./compose.yml --auto-update
```

When a newer release is detected, compose-remote replaces its own binary
and calls `os.Exit(0)`. The process supervisor (pm2, systemd, etc.) then
restarts it with the new binary. The docker-compose stack keeps running
uninterrupted throughout.

Update checks are skipped for development builds (`version = "(devel)"`).

## Running under pm2

See `ecosystem.config.example.js` for a starting point. Key idea: one pm2
app per stack, log to stdout (compose-remote already emits structured
key/value lines), let pm2 capture and rotate.

```sh
pm2 start ecosystem.config.example.js
pm2 logs my-stack
```

## Logging

All output is structured key/value, one event per line, written to
stdout (info) or stderr (warn/error). Set `COMPOSE_REMOTE_DEBUG=1` for
verbose docker-call logging.

## State

`<state-dir>/<name>/` contains:

- `compose.yml` — the last fetched compose content with hash labels
  injected. This is the file passed to `docker compose -f`.
- `git/` — the persistent shallow clone for git sources.

Nothing else is persisted. There is intentionally no "last applied" hash
file; the source of truth for "what's currently applied" is the running
containers' labels.

## CI

CI uses `wow-look-at-my/go-toolchain@v1`; see `.github/workflows/ci.yml`.

## License

MIT.

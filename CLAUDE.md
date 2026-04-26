# compose-remote

A small Go daemon that reconciles a docker-compose stack against a remote
source (file/url/git). Built around a Kubernetes-style reconcile loop:
every tick we compare DESIRED state (the compose file as currently
sourced) to ACTUAL state (running containers) and resolve any difference.
Includes a bug-fix pass to force-recreate services when docker compose
wrongly reports "up-to-date".

## Layout

```
cmd/                       # cobra commands; one command per file
  root.go                  # rootCmd + Execute()
  run.go                   # `run` daemon loop subcommand
  apply.go                 # `apply` one-shot subcommand
  update.go                # `update` one-shot self-update subcommand
  version.go               # `version` subcommand (also exposes currentVersion())

internal/log/              # tiny key=value structured logger
internal/secrets/          # sops-encrypted dotenv loader (--sops-env-file)
internal/source/           # Source interface + file/http/git backends
internal/state/            # state-dir layout (compose.yml, git clone)
internal/compose/          # docker compose wrapper + service hashing
internal/reconcile/        # diff + apply pipeline (bug-fix pass)
internal/runner/           # the reconcile loop (Tick, Run, RunOnce)

main.go
```

## Build / test

Always use `go-toolchain` (no arguments) at the repo root. It runs
`go mod tidy`, `go vet`, `go test -cover`, and `go build`. Coverage must
stay above 80%.

Do NOT run `go build`, `go test`, `go mod tidy`, etc. directly.

## Architectural rules

- Every `docker compose up` we issue MUST include
  `--remove-orphans --wait`. The `--wait` is non-negotiable: without it
  the bug-fix pass races against unstarted containers.
- Pulls only happen for services whose image string changed in the YAML
  (`reconcile.PullSet`). No periodic blanket pulls.
- Sources are the only mutable input; everything else is derived. Source
  fetches must be cheap on the no-change path (HTTP ETag, git fetch on a
  shallow clone, file mtime check).
- `internal/compose.LabelHash` is injected onto every service before
  writing the compose file. We compare actual vs. desired by reading
  this label off running containers — we do NOT trust docker compose's
  own `com.docker.compose.config-hash`.
- `serviceHash` MUST be stable across `Parse` -> `Marshal` -> `Parse`
  round-trips. `stripOwnLabel` exists for exactly this reason.
- `reconcile.Apply` takes a `Composer` interface (not the concrete
  `*compose.Client`) so it can be unit-tested with a fake. Same for
  `runner.Tick`.

## Secrets

`--sops-env-file <path>` (repeatable) tells the daemon to shell out to
`sops decrypt <path>` at startup and export the resulting KEY=VALUE
pairs into its own process environment. Docker compose, invoked as a
child, then inherits those vars and uses them for `${VAR}` substitution
in the compose YAML. Plaintext never hits disk.

Three storage formats are supported, distinguished by file extension:

- `.json` — top-level JSON object. **Default / recommended** because
  JSON's quoting rules are unambiguous (no surprises around `=`, `#`,
  multiline strings, etc.).
- `.yaml` / `.yml` — top-level YAML mapping.
- `.env` — dotenv-style `KEY=VALUE`.

Any other extension is rejected outright. Inside JSON/YAML, only
top-level scalars are allowed (string, bool, int, null); nested objects,
arrays, and fractional numbers are rejected with a clear error rather
than silently flattened. Env vars are flat strings — there is no sane
universal mapping from structured data to them, so we make the caller
choose.

Decryption happens once at startup. To rotate a secret, the supervisor
(pm2/systemd) must restart the daemon — the next reconcile picks up the
new value.

We deliberately shell out to the `sops` binary rather than importing
sops as a Go library: the latter pulls in a huge tree of cloud-KMS
clients we don't need. Operators using sops already have the binary.

## Self-update

The daemon can update itself via `go-selfupdate-mini`:

- `compose-remote update` — one-shot: detect the latest GitHub release and
  replace the running binary, then exit.
- `compose-remote run --auto-update` — enable background update checking on
  a ticker (default `--auto-update-interval 1h`). When a newer release is
  found, the binary is replaced in-place and the process calls `os.Exit(0)`.
  pm2 (or whatever supervisor) then restarts it with the new binary.

The docker-compose stack is unaffected by a restart of compose-remote itself.

Update checks are skipped silently when `currentVersion()` returns `"(devel)"`
(i.e. a binary built directly from source rather than from a tagged release).

## Adding a new source backend

1. Implement the `source.Source` interface in
   `internal/source/<name>.go`.
2. Add a flag and case to `internal/source/factory.go`.
3. Wire the flag into `cmd/run.go` and `cmd/apply.go` via
   `addSourceFlags`.
4. Test against a fixture (httptest.Server, t.TempDir bare git repo,
   etc.).

## CI

Standard go-toolchain workflow at `.github/workflows/ci.yml`. The
workflow uses `autorelease: true`, so `permissions:` MUST include
`id-token: write` AND `contents: write` (the `write` is required by the
autorelease step — do not downgrade it to `read`).

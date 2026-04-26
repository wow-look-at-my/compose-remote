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
  version.go               # `version` subcommand

internal/log/              # tiny key=value structured logger
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

## Adding a new source backend

1. Implement the `source.Source` interface in
   `internal/source/<name>.go`.
2. Add a flag and case to `internal/source/factory.go`.
3. Wire the flag into `cmd/run.go` and `cmd/apply.go` via
   `addSourceFlags`.
4. Test against a fixture (httptest.Server, t.TempDir bare git repo,
   etc.).

## CI

Standard go-toolchain workflow at `.github/workflows/ci.yml`. Do not
touch `permissions: id-token: write, contents: read` — required by
go-toolchain.

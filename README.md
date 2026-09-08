# argo-tui

A keyboard-first terminal UI for Argo Workflows. It can inspect workflow lists and details, render deterministic node outlines and sanitized resources, and stream bounded logs through an Argo Server REST client. The beta watch/action wave is present locally, but Q2 found release-blocking beta defects; this handoff is an alpha-quality local build, not a beta promotion.

## Current release status

Version: `0.1.0-alpha`

- Read-only list, detail, node, resource and log journeys are covered by synthetic/fake-server tests and a Linux PTY smoke journey.
- Demo mode performs no network I/O and no writes: `--demo` uses only the built-in synthetic reader.
- TLS verification, custom CA, token environment-variable/file sources, base paths, pagination, log framing and terminal-text sanitization are tested locally.
- Q2 did not approve beta: confirmed action intents currently do not reach the root executor, and watch 401/403/429 failures can reconnect without terminal auth handling/backoff. See `docs/reviews/beta.md`.
- No real Argo Server or disposable Argo environment was available. Compatibility with a deployed Argo version is therefore unverified, not supported by inference.

## Quick start

Requires Go >= 1.25 (the module pins Go 1.25.0 and toolchain go1.25.4).

Run the offline demo:

```sh
go build -o dist/argo-tui ./cmd/argo-tui
./dist/argo-tui --demo
./dist/argo-tui --version
```

The demo uses synthetic data and never contacts a server. Press `q` to quit.

A real connection uses placeholder values only; provide credentials through an environment variable or token file, never by putting the token in the command line:

```sh
export ARGO_TUI_TOKEN='placeholder-token'
./dist/argo-tui \
  --server https://argo.example.test/argo \
  --namespace workflows
```

The endpoint and namespace must be supplied by the operator. SSO browser login and direct Kubernetes access are not implemented. Do not use the placeholder endpoint or token against production.

## Local verification

The release handoff was verified on Linux amd64 with Go 1.25.4:

```sh
gofmt -l cmd internal tests
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go test -tags=integration ./tests/integration/... -count=1 -v
go test ./internal/ui/detail -bench BenchmarkNodeOutline -benchmem -run '^$'
go test ./internal/ui/logs -bench BenchmarkBuffer -benchmem -run '^$'
```

The integration suite exercised the built binary in a Linux PTY, including demo list rendering, `q`/Ctrl-C exit, version output, small-terminal behavior, terminal restoration bytes and the demo no-egress guard. Its server tests use local synthetic HTTP fixtures only. `make` was not available in the release environment, so the equivalent Go commands above were run directly.

## Build artifacts

Local, ignored artifacts are written to `dist/`; they are not tagged, uploaded or published. The checked source commit and exact artifact checksums are recorded in the Kanban handoff. The local build produced:

- `dist/argo-tui` — Linux amd64 host binary; executed for `--version`.
- `dist/argo-tui-linux-amd64` — Linux amd64 cross-build; same source output as the host binary.
- `dist/argo-tui-darwin-amd64` — macOS amd64 cross-build; not executed on macOS.
- `dist/argo-tui-windows-amd64.exe` — Windows amd64 cross-build; not executed on Windows.

## Documentation

- `docs/usage.md` — operator usage and interaction notes.
- `docs/security.md` — credential, TLS, redirect and terminal-sanitization posture.
- `docs/compatibility.md` — tested versus cross-compiled environments and unsupported claims.
- `docs/reviews/alpha.md` and `docs/reviews/beta.md` — independent gate evidence and limitations.
- `CHANGELOG.md` — local alpha handoff changes.

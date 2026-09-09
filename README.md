# argo-tui

A keyboard-first terminal UI for Argo Workflows. It can inspect workflow lists and details, render deterministic node outlines and sanitized resources, stream bounded logs, recover watches, and run explicitly enabled retry/resubmit/stop/terminate journeys through an Argo Server REST client. This is a local beta candidate, not an operational deployment claim.

## Current release status

Version: `0.2.0-beta.1`

- Read-only list, detail, node, resource and log journeys are covered by synthetic/fake-server tests and a Linux PTY smoke journey.
- Demo mode performs no network I/O and no writes: `--demo` uses only the built-in synthetic reader.
- TLS verification, custom CA, token environment-variable/file sources, base paths, pagination, log framing and terminal-text sanitization are tested locally.
- The independent final beta approval passed the local action, watch-recovery, transport, security, integration, race, vet, benchmark and build gates. See `docs/reviews/beta-approval.md`.
- No authorized disposable Argo environment was available. Live compatibility remains explicitly blocked; synthetic fixtures never establish deployment compatibility.

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

The beta handoff was verified on Linux amd64 with Go 1.25.4. Required commands and their logs are recorded in the local handoff evidence:

```sh
gofmt -l cmd internal tests
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go test -tags=integration ./tests/integration/... -count=1 -v
go test ./internal/ui/detail -bench BenchmarkNodeOutline -benchmem -run '^$'
go test ./internal/ui/logs -bench BenchmarkBuffer -benchmem -run '^$'
go build -trimpath -o dist/argo-tui ./cmd/argo-tui
./dist/argo-tui --version
```

The integration suite exercised the built binary in a Linux PTY, including demo list rendering, action/watch regression paths, `q`/Ctrl-C exit, version output, small-terminal behavior, terminal restoration bytes and the demo no-egress guard. Its server tests use local synthetic HTTP fixtures only.

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
- `docs/reviews/alpha.md`, `docs/reviews/beta.md` and `docs/reviews/beta-approval.md` — independent gate evidence and limitations.
- `CHANGELOG.md` — local beta handoff changes.

# Development and testing

## Architecture

The app uses Bubble Tea v2 and a standard-library HTTP adapter for Argo's REST
API. It has no generated Argo SDK or in-process Kubernetes client. Kubernetes
access for managed forwarding goes through `kubectl`.

| Package | Responsibility |
| --- | --- |
| `cmd/argo-tui` | Flags, config, connection setup and process lifecycle |
| `internal/config`, `internal/portforward` | Profile validation and owned port-forward recovery |
| `internal/core` | Reader, Watcher and Actioner interfaces; workflow types and errors |
| `internal/argo` | REST transport, pagination, watch/log parsing, pod resolution and mutations |
| `internal/app` | Routes, async commands, stale-response rejection and action orchestration |
| `internal/ui` | List, detail, logs, namespaces, actions and shared rendering |
| `internal/testkit` | In-memory reader, synthetic demo data and injectable clock |

Keep API declarations in the Go source rather than duplicating them in docs.
Dependency versions live in [go.mod](../go.mod); CI tool versions and jobs
live in [ci.yml](../.github/workflows/ci.yml).

### Transport and state rules

- Workflow identity includes namespace, name and UID. Detail requests pass the
  selected UID; mutations perform an identity preflight but cannot impose a
  server-side UID precondition on name-addressed action endpoints.
- Continuation tokens and resource versions are opaque. The app rejects stale
  async responses and marks incomplete snapshots rather than silently treating
  them as complete.
- List and detail use `/api/v1/workflows/{namespace}` and its `/{name}` child.
  Watch uses `/api/v1/workflow-events/{namespace}`. Logs use workflow or pod
  `/log` endpoints. Configured base paths are preserved.
- Watch and log parsers accept JSON-lines and SSE envelopes, including in-band
  errors after HTTP 200. Stream cancellation is distinct from transport failure.
  Watch recovery belongs to the app; expired resource versions require a relist.
- Resume, Suspend, Retry, Resubmit, Stop and Terminate use the PUT
  `/api/v1/workflows/{namespace}/{name}/{action}` endpoints; Delete uses
  DELETE `/api/v1/workflows/{namespace}/{name}` with no body. The server builds
  its own delete options, so no UID precondition can be sent with a delete
  either. Writes are sent once; ambiguous results remain unknown. The UI
  requires session opt-in and confirmation; demo mode cannot write. A bulk
  action is a sequence of single requests, each with its own preflight and
  read-back. Every attempt is appended to the action journal
  (`internal/journal`), whose failures are reported and never block a write.
- A workflow counts as suspended when it holds a running Suspend node or has
  not finished and has `spec.suspend` set, the two states Argo's resume clears.
  The list projection carries `spec.suspend`; the gate scan adds the node half.
- Unknown workflow fields are retained in raw resource JSON. Missing timestamps
  and unavailable node status must remain distinguishable from zero values or
  an empty workflow. Node IDs are not assumed to be pod names; naming is resolved
  in the adapter and covered by pod-name tests.
- Untrusted terminal text passes through shared sanitization. Resource parameter
  and output values are hidden by default. These controls do not guarantee that
  arbitrary application logs contain no secrets.

The adapter and fixtures target Argo Workflows v4.1.2. See the
[README](../README.md#troubleshooting-and-limits) for the scope of recorded
live testing. Synthetic test results do not establish deployment compatibility.

## Local checks

Run from the repository root with Go installed:

```sh
go test ./...
go vet ./...
gofmt -l cmd internal tests
go test -race ./...
go test -tags=integration ./tests/integration/... -count=1
go build -o dist/argo-tui ./cmd/argo-tui
./dist/argo-tui --version
```

`gofmt -l` should print nothing. The race detector needs a supported platform
and C toolchain. `make test` and `make test-race` cover a focused package set,
not the whole repository. `make build` includes the current Git commit.

Refresh golden files only for intentional rendering changes:

```sh
UPDATE_GOLDEN=1 go test ./...
```

Review the resulting diff. Commands using `NAME=value command` assume a POSIX
shell; in PowerShell set `$env:NAME` before running the command.

### Test coverage

| Suite | Scope and prerequisites |
| --- | --- |
| Default `go test ./...` | Package tests, production-adapter HTTP fixtures and connected-path regressions |
| `tests/connected` | Fake kubectl plus a synthetic server; tests using the fake executable skip without `/opt/homebrew/bin/fish` |
| `tests/integration` | Tagged fixture-server/root-model tests and real-binary PTY tests; the wire harness uses its own test client |
| `tests/e2e` | Tagged tests with an explicit gate for the real-cluster submit/list/detail/delete journey |

PTY helpers are provided for Linux and macOS; terminal availability can cause
skips. Inspect test output before claiming that a suite exercised every path.
The integration wire client does not replace the production adapter's own tests.

## Real-cluster tests

Use a disposable Argo environment and a dedicated namespace. The E2E journey
creates and deletes workflows; it is not a read-only smoke test. Provision the
server and namespace separately and arrange a reachable endpoint before running
the suite. These tests do not start the app's managed forward.

Copy [the allowlist example](../tests/e2e/e2e-config.example.yaml) to a local,
untracked file. Despite its `.yaml` suffix, the harness parses `key=value`
lines, not YAML. Set `server`, `namespace` and `allowActions=true`. The optional
`token` field contains the token value, not a filename; keep real credentials
out of Git.

```sh
ARGO_TUI_E2E=1 ARGO_TUI_E2E_CONFIG=/absolute/path/to/e2e-config.yaml \
  go test -tags=e2e ./tests/e2e/... -count=1 -v
```

The real journey requires the build tag, environment opt-in and valid allowlist
with explicit mutation enablement. A closed gate skips the journey with a reason;
it is not evidence of a live pass. Gate and client unit tests can still run.

The journey uses a separate HTTP Reader defined in `tests/e2e/client.go` and
checks server version. Despite its name,
`productionReader` does not use `internal/argo.Client` and does not implement
log streaming. Cleanup deletes by name without checking ownership labels or UID.
The current fixture uses `generateName`, but the journey waits for and deletes
the fixed name `argo-tui-e2e-hello` without reading the created name from the
submit response. This can fail the journey and leave its workflow behind.
Resolve that mismatch before enabling live runs. Use an isolated, disposable
namespace with no existing workflows; fixtures under `tests/e2e/testdata`
are synthetic.

CI runs format, vet, unit, integration, race and build checks. E2E runs only on
manual workflow dispatch with `E2E_ALLOWLIST_B64` configured. Installing kind
and kubectl in that job does not provision a cluster; the allowlisted endpoint
must already be reachable from the runner.

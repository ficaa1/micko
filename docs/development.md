# Development and testing

## Release demo automation

After a successful stable release, `release.yml` calls `demo.yml` with the
release tag. The recorder builds that tag with `make build`, records its
demo tape, and copies only `docs/demo.gif` onto a branch based on current
`main`. It opens a PR and enables auto-merge after the required CI check.
Older release reruns leave the latest release's demo alone.

The repository must have auto-merge enabled. The recorder uses a GitHub App
installation token so its PR starts CI without manual approval. Register a
private app at <https://github.com/settings/apps/new> with the repository
homepage, webhooks disabled, and Contents and Pull requests permissions set
to Read & write. Install it only on `ficaa1/micko`. It needs no ruleset bypass.

Save its numeric App ID as the repository variable `DEMO_APP_ID`. Generate a
private key on the app's settings page and upload the downloaded PEM file:

```sh
gh variable set DEMO_APP_ID --repo ficaa1/micko --body APP_ID
gh secret set DEMO_APP_PRIVATE_KEY --repo ficaa1/micko < /path/to/private-key.pem
```

To re-record the latest release without publishing again:

```sh
gh workflow run demo.yml --repo ficaa1/micko --ref main
```

## Architecture

The app uses Bubble Tea v2 and a standard-library HTTP adapter for Argo's REST
API. It has no generated Argo SDK or in-process Kubernetes client. Kubernetes
access for managed forwarding goes through `kubectl`.

| Package | Responsibility |
| --- | --- |
| `cmd/micko` | Flags, config, connection setup and process lifecycle |
| `internal/config`, `internal/setup` | Profile validation, guided creation and YAML-preserving persistence |
| `internal/session`, `internal/portforward` | Connection ownership, bounded read-only checks and port-forward recovery |
| `internal/core` | Reader, Watcher and Actioner interfaces, the optional listers of the other kinds and the archive; workflow, cron workflow and template types and errors |
| `internal/cronexpr` | The controller's cron dialect: parsing and next run times in a time zone |
| `internal/argo` | REST transport, pagination, watch/log parsing, pod resolution and mutations |
| `internal/app` | Routes, async commands, stale-response rejection, action orchestration and the command registry |
| `internal/ui` | List, detail, logs, namespaces, profiles, command palette, actions and shared rendering; `kindlist` is the list of the other kinds, `cronlist`, `templatelist` and `archivedlist` its kinds |
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
- The all-namespaces view sends an empty namespace: `/api/v1/workflows/` and
  `/api/v1/workflow-events/`, trailing slash included, which Argo's route
  matches with an empty namespace. The server checks the token's permission to
  list cluster-wide and answers 403 without it. A server with a
  `managedNamespace` in `/api/v1/info` is refused the request by the adapter,
  because its answer would cover only that namespace.
- A palette command or a resource kind is one entry in `internal/app/palette.go`:
  `builtinCommands` for a command, `listKinds` for a kind with its own list
  route. The palette package ranks and completes whatever the registry holds.
  A kind beside workflows also needs a narrow lister interface in
  `internal/core`, implemented by the adapter and the fake, a
  `kindlist.Spec` for its columns, sorts and info panel, and a `kindDef` in
  `internal/app` (see `cron.go`).
- Cron workflows are listed from `/api/v1/cron-workflows/{namespace}`, paged
  with `listOptions.limit` and `listOptions.continue`; that endpoint takes no
  `fields` projection. Their drill-down is the workflow list with
  `listOptions.labelSelector=workflows.argoproj.io/cron-workflow=<name>`.
- Workflow templates come from `/api/v1/workflow-templates/{namespace}` and
  cluster templates from `/api/v1/cluster-workflow-templates`, paged the same
  way and without a projection. Their drill-downs select on
  `workflows.argoproj.io/workflow-template` and
  `workflows.argoproj.io/cluster-workflow-template`.
- The archive is `/api/v1/archived-workflows`, narrowed with
  `listOptions.fieldSelector=metadata.namespace=<ns>` (every server version
  reads it; v3.5+ also takes a `namespace` parameter) and paged with an offset
  token; a run is read from `/api/v1/archived-workflows/{uid}`. With no archive
  configured the server's null archive answers the list with an empty page and
  the detail with a 500 "getting archived workflows not supported"; the adapter
  reports that, and a missing route (404 on the list, 501), as
  `core.ArchiveDisabledMessage`.
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
- Template and cron mutations use `core.ResourceReader` for identity
  preflight and `core.ResourceActioner` for a single send. Submissions use
  POST `/api/v1/workflows/{namespace}/submit` with `resourceKind`,
  `resourceName` and `submitOptions.parameters`; cron suspend/resume use
  PUT `/api/v1/cron-workflows/{namespace}/{name}/{suspend|resume}`.
  Originating resource identity and workflow destination are distinct for
  cluster templates. A subsequent read promotes ACCEPTED to CONFIRMED;
  ambiguous writes remain UNKNOWN and are never resent. These contracts
  are verified against the pinned upstream
  [workflow protobuf](https://github.com/argoproj/argo-workflows/blob/v4.1.2/pkg/apiclient/workflow/workflow.proto),
  [cron protobuf](https://github.com/argoproj/argo-workflows/blob/v4.1.2/pkg/apiclient/cronworkflow/cron-workflow.proto),
  [submit options](https://github.com/argoproj/argo-workflows/blob/v4.1.2/pkg/apis/workflow/v1alpha1/common.go)
  and [submission handler](https://github.com/argoproj/argo-workflows/blob/v4.1.2/server/workflow/workflow_server.go).
- A workflow counts as suspended when it holds a running Suspend node or has
  not finished and has `spec.suspend` set, the two states Argo's resume clears.
  The list projection carries `spec.suspend`; the gate scan adds the node half.
- Unknown workflow fields are retained in raw resource JSON. Missing timestamps
  and unavailable node status must remain distinguishable from zero values or
  an empty workflow. Node IDs are not assumed to be pod names; naming is resolved
  in the adapter and covered by pod-name tests.
- Untrusted terminal text passes through shared sanitization. Parameter and output
  values are shown unless the profile sets `redactValues` or the session was
  started with `--redact-values`. These controls do not guarantee that
  arbitrary application logs contain no secrets.

Explain and Events share one pair of cancelable event streams per selected
workflow. `diagnose.Input` accepts event snapshots and collection status; its
warning cards require workflow UID identity or a unique verified pod name plus
a creation-time bound. Event order, replay replacement and eviction ties are
deterministic. Event coverage is additive to workflow and log rules.

Compare uses `core.Query.SummaryOnly` to skip the list adapter's supplemental
suspended-node scan. Its baseline search is namespace/lineage scoped, bounded
by three pages, 300 summaries and a 20-second context; only the selected baseline
gets a detail GET. Async replies must match connection/selection generations,
request ID and full workflow identity. The detail pane reuses the production
outline and report renderer for path matching, scrolling, sanitization and
redacted clipboard output. Retry/fanout instances have explicit ambiguous
alignment; template scope and step-group indexes qualify stable task paths.


The adapter and fixtures target Argo Workflows v4.1.2. See the
[usage guide](usage.md#limits-and-troubleshooting) for the scope of recorded
live testing. Synthetic test results do not establish deployment compatibility.

## Local checks

Run from the repository root with Go installed:

```sh
go test ./...
go vet ./...
gofmt -l cmd internal tests
go test -race ./...
go test -tags=integration ./tests/integration/... -count=1
go build -o dist/micko ./cmd/micko
./dist/micko --version
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
| `tests/connected` | POSIX-only managed port-forward and production client against a sh fake kubectl and a synthetic server |
| `tests/integration` | Tagged root-model journeys over the production client against a fixture server, and real-binary PTY tests |
| `tests/e2e` | Tagged tests with an explicit gate for the external-cluster submit/list/detail/delete journey |
| `tests/compat` | `compat`-tagged deployment journeys provisioned by `scripts/compat/argo.sh`; Docker, kind, kubectl, curl, OpenSSL, Python 3 and Go required |

PTY helpers are provided for Linux and macOS; terminal availability can cause
skips. Inspect test output before claiming that a suite exercised every path.

## Performance

[performance.md](performance.md) covers the request timings of `--debug`, the
scripted sessions in `scripts/perf` and the benchmarks.

## Disposable deployment compatibility

Run `scripts/compat/argo.sh --check` to check prerequisites, then
`scripts/compat/argo.sh` from a Linux or macOS host with Docker running.
Docker Desktop must expose loopback-published ports on macOS. The runner
creates a uniquely named kind cluster with its own temporary kubeconfig;
every kubectl call explicitly names that context and file. It creates
`argo` and `micko-compat` namespaces in that cluster, labels the workload
fixtures, and deletes the cluster and private temporary state on exit,
including setup or test failure. It never selects or changes an existing
cluster. A forced process kill or host failure cannot run a shell trap;
remove a leftover cluster with `kind delete cluster --name NAME` using the
printed `micko-compat-...` name.

The runner pins the official Argo **v4.1.2 release asset** `install.yaml`
by SHA-256 (`adac8370b9c0470acd3a71cf4139d40d7b8e995a774c1a639864b3db0e601a83`).
The tag resolves to commit `16a52d67daf2f4a8a76fa8bec02a76a46aa46257`.
It uses the release's versioned server/controller images; the source-tree
quick-start manifest contains `latest` images and is not used. Kubernetes
uses kind v0.33.0's published v1.37.0 node image and digest. Sources:
[Argo release](https://github.com/argoproj/argo-workflows/releases/tag/v4.1.2),
[kind release and node digest](https://github.com/kubernetes-sigs/kind/releases/tag/v0.33.0).
No downloaded manifest is applied until its checksum matches.

The server runs in client auth mode with a short-lived locally issued CA and
IP-valid TLS certificate. Service-account bearer tokens live only in private
temporary files. The tests use the production `session.Connector` and Argo
transport for direct HTTPS through a loopback NodePort, and separately through
the app's managed kubectl port-forward. They verify the pinned server version,
CA rejection, missing/invalid token (401), a token with no namespace access
(403), both template submissions, cron run-now and suspend/resume, completion,
parameter delivery in logs, list/detail identity, deletion, and closing the
managed listener. Cleanup checks workflow UID and fixture ownership before
removing an individual run; cluster deletion is the final boundary. Artifact
stores, archive persistence, SSO and a Windows terminal are outside this suite.
A live failure fails the job; a requested deployment run cannot succeed by
skipping for a missing secret or endpoint.

CI's manual `workflow_dispatch` input `deployment_compatibility` provisions
this environment on its own; it needs no repository secrets or external
cluster. Routine CI checks the runner's shell syntax and runs fake-tool
setup/cleanup tests, including setup, checksum and journey failure:

```sh
bash -n scripts/compat/argo.sh
curl -fsSL https://github.com/argoproj/argo-workflows/releases/download/v4.1.2/install.yaml -o /tmp/argo-v4.1.2-install.yaml
python3 scripts/compat/test_runner.py /tmp/argo-v4.1.2-install.yaml
```

These fake-tool checks verify isolation and cleanup; they do not establish
Argo deployment compatibility. `go test -tags=compat ./tests/compat/...`
without the runner reports an explicit skip and is compile/gate validation
only.

### Recorded local validation

On 2026-10-28 in the Hermes Linux LXC, these checks passed:

```sh
gofmt -l .
git diff --check
go test ./...
go test -race ./... -count=1
go test -tags integration ./tests/... -count=1 -v
go vet ./...
go vet -tags integration ./...
go vet -tags e2e ./...
go vet -tags compat ./...
go run honnef.co/go/tools/cmd/staticcheck@latest -checks U1000 -tags integration,e2e,compat ./...
GOOS=windows GOARCH=amd64 go build -o /tmp/micko-compat-windows.exe ./cmd/micko
GOOS=windows GOARCH=amd64 go test -exec=true -tags=integration,e2e,compat ./...
bash -n scripts/compat/argo.sh
/tmp/shellcheck-v0.11.0/shellcheck scripts/compat/argo.sh
python3 scripts/compat/test_runner.py /tmp/micko-argo-install.yaml
```

The integration run executed its Linux PTY journeys without skips. The
Windows commands cross-compiled executable and test binaries; `-exec=true`
did not execute Windows code. Shellcheck v0.11.0 was downloaded to `/tmp`;
the manifest supplied to the Python test was the checksum-verified upstream
release asset. Parsing both workflow and fixture YAML and checking the
release's server/controller image tags also passed.

`go test -tags=e2e ./tests/... -count=1 -v` passed its gate tests and skipped
the external live journey because `MICKO_E2E` was unset.
`go test -tags=compat ./tests/compat/... -count=1 -v` compiled and explicitly
skipped the deployment journey. `scripts/compat/argo.sh --check` exited 1,
reporting missing **docker, kind and kubectl**. The real deployment, direct
HTTPS/auth, managed real-cluster forwarding and native Windows execution
remain unverified here. Reproduce the deployment checks with
`scripts/compat/argo.sh` on a host with those tools and Docker running, or
select `deployment_compatibility` in the manual CI dispatch.

Mutation checks were run and restored: removing saved-view namespace
restoration failed the palette roundtrip; removing resource UID preflight
failed the replacement-refusal journey; removing cluster deletion failed
all four runner cleanup scenarios. Their restored checks passed.

### Windows scope

CI runs Windows build/startup smoke, default unit and HTTP transport tests,
and the integration fixture-server journeys. POSIX fake kubectl and Linux/macOS
PTY files have platform constraints. Native home-directory tests use Windows'
`USERPROFILE` in temporary environments. This covers executable startup and
model/transport behavior; it does not claim Windows PTY, managed-forward live
or live-cluster coverage. Log piping still requires `/bin/sh`.

## External-cluster tests

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
MICKO_E2E=1 MICKO_E2E_CONFIG=/absolute/path/to/e2e-config.yaml \
  go test -tags=e2e ./tests/e2e/... -count=1 -v
```

The real journey requires the build tag, environment opt-in and valid allowlist
with explicit mutation enablement. A closed gate skips the journey with a reason;
it is not evidence of a live pass. The gate's own tests still run.

The journey lists, reads and deletes through the production client,
`internal/argo.Client`; the harness submits the fixture and polls its phase with
its own requests, for arbitrary workflow-manifest fixtures; product submission starts from templates or cron workflows. The fixture uses
`generateName`, and every step addresses the name the server assigned. The
cleanup delete, which runs even when a step fails, goes by that name without
checking ownership labels or UID. Use an isolated, disposable namespace with no
existing workflows; fixtures under `tests/e2e/testdata` are synthetic.

CI runs format, vet (including integration, E2E and compatibility tags),
unused-code staticcheck, unit, integration, race and build checks. The pinned
staticcheck v0.8.2 tool selects Go 1.26 automatically for its own build; the
application continues to target the version in go.mod. External E2E
requires the manual `external_e2e` dispatch input and `E2E_ALLOWLIST_B64`;
requesting it without a secret fails. The allowlisted endpoint must already
be reachable. It remains separate from the self-provisioning deployment job.

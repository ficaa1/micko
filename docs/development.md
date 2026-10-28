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
`scripts/compat/argo.sh` from a Linux or macOS host with Docker running
(on macOS, Docker Desktop must expose loopback-published ports). The runner
creates a uniquely named kind cluster with its own temporary kubeconfig, never
touches an existing cluster, and deletes the cluster and its temporary state
on exit, including after a failure. A forced kill cannot run the trap; remove
a leftover cluster with `kind delete cluster --name NAME` using the printed
`micko-compat-...` name.

The runner applies the official Argo v4.1.2 release `install.yaml` only after
its SHA-256 matches the value pinned in `argo.sh`, on kind v0.33.0's
Kubernetes v1.37.0 node image. The tests in `tests/compat` go through the
production `session.Connector`, both over direct HTTPS and through the managed
kubectl port-forward, and cover TLS and token failures, template submission,
cron run-now and suspend/resume, and list, detail and delete. Artifact stores,
archive persistence, SSO and a Windows terminal are outside the suite.

CI runs this on a manual `workflow_dispatch` with `deployment_compatibility`;
it needs no secrets. Routine CI runs `scripts/compat/test_runner.py`, which
drives the runner against fake tools to check isolation and cleanup, not Argo
compatibility. Without the runner, `go test -tags=compat ./tests/compat/...`
skips.

CI on Windows runs the build and startup smoke, the unit tests and the
fixture-server journeys. PTY, managed forwarding and live-cluster tests are
POSIX-only, and log piping needs `/bin/sh`.

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
unused-code staticcheck, unit, integration, race and build checks. External E2E
requires the manual `external_e2e` dispatch input and `E2E_ALLOWLIST_B64`;
requesting it without a secret fails. The allowlisted endpoint must already
be reachable. It remains separate from the self-provisioning deployment job.

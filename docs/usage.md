# Usage — argo-tui (v0.1 read-only alpha)

Operator-facing quickstart and command reference. Status: the F1
baseline binary ships the demo mode and the frozen CLI surface; the full
alpha UI is assembled by I1 (list/detail/logs views exist as worker
components on their branches). This page documents the target v0.1
behavior and what works today; nothing here promises unreleased features.

## 1. Install / build

```sh
go build -o dist/argo-tui ./cmd/argo-tui   # or: make build
./dist/argo-tui --version
```

Go 1.25.x (pinned in `go.mod`); no external runtime dependencies.

## 2. Quickstart: offline demo

```sh
./dist/argo-tui --demo
```

`--demo` runs against the in-memory fake Reader (5 synthetic workflows).
It performs **no network I/O** (verified by the PTY egress guard), never
reads credentials, and never writes. `q` quits, Ctrl-C quits anywhere.

A real connection in v0.1 requires a configured profile (below); starting
without `--demo` and without a usable profile prints guidance and exits 1
— it never falls back to the network implicitly.

## 3. Configuration

Config file (resolved via `os.UserConfigDir()`, i.e.
`~/.config/argo-tui/config.yaml` on Linux):

```yaml
currentProfile: dev
refreshInterval: 5s
profiles:
  dev:
    server: https://argo.example.test/argo
    namespace: workflows
    tokenEnv: ARGO_TUI_TOKEN        # mutually exclusive with tokenFile
    # tokenFile: /absolute/path/to/token
    # caFile: /absolute/path/to/ca.pem
    # namespaces: [workflows, batch]  # optional picker candidates
```

Precedence: **explicit CLI flag > selected profile > config default**;
missing endpoint/namespace blocks startup before the alternate screen.
No implicit `default` namespace, no implicit all-namespaces scan.

### Flags (frozen set, plan §3)

| Flag | Meaning |
|---|---|
| `--config` | alternate config file path |
| `--profile` | profile selection |
| `--server` | Argo Server base URL (base path preserved) |
| `--namespace` | namespace to inspect (required for a real run) |
| `--token-file` | bearer-token file (re-read per request; rotation-safe) |
| `--ca-file` | custom CA bundle |
| `--refresh-interval` | poll interval (default 5s, bounded below) |
| `--insecure-skip-tls-verify` | explicit opt-out; permanent on-screen warning |
| `--allow-actions` | v0.2 only: per-invocation write enablement (absent in v0.1) |
| `--demo` | offline fake backend (no network, no writes) |
| `--version` | print version and exit |

Tokens: use `tokenEnv` (environment-variable name) or `tokenFile` —
never literal tokens in config or on the command line. See
[`security.md`](security.md) for the full credential/TLS contract.

## 4. Keyboard map (v0.1)

| Key | Action |
|---|---|
| `j`/`k` or arrows | move selection |
| Enter | open selected workflow (detail) |
| Esc | back out / cancel (logs → detail → list) |
| Tab | switch detail tabs |
| `/` | focus local search (list); searches the collected snapshot only |
| `n` | namespace input/picker (manual names allowed even if enumeration is forbidden) |
| `p` | profile picker |
| `r` | refresh (coalesced while in flight) |
| `?` | help |
| Space (logs) | pause autoscroll (collection continues, bounded) |
| `f` (logs) | resume follow |
| `a` | (v0.2) actions menu — absent/disabled in v0.1 |
| `q` | quit — only outside text entry |
| Ctrl-C | quit globally |

## 5. What you see

- Header: `argo-tui | <profile> | <server> | ns: <namespace> | READ ONLY`.
- List: name, phase, age, duration; local search/sort over the collected
  snapshot; the scope of search is shown (it does not claim to search
  uncollected workflows); snapshot-cap/continuation limits are marked
  incomplete rather than silently truncated.
- Detail: phase, age/duration, message, labels, arguments, node outline
  (deterministic order; DAG shared children shown via reference rows).
- Resource: normalized YAML of the server-returned JSON with parameter/
  output values collapsed by default; reveal is explicit and session-only.
- Logs: follow/pause/search over the retained buffer (10k lines / 8 MiB
  caps), container context explicit (`main` default, editable).
- Every failure is a distinguishable state: loading, empty, forbidden,
  unauthenticated, offline/stale (with stale age and last good data),
  deleted ("workflow no longer available"), unsupported, partial/incomplete.

## 6. Connecting to a real Argo Server (when the alpha UI ships)

```sh
export ARGO_TUI_TOKEN="<service-account token>"   # value never in config
./dist/argo-tui --profile dev --namespace workflows
```

Requirements and caveats — see [`security.md`](security.md) for the
normative detail:

- Argo Server v4.1.x is the tested target (`/api/v1/version` is checked).
- `client` auth mode: pass a Kubernetes bearer token via
  `tokenEnv`/`tokenFile`. `server`/`sso` modes: use credentials that
  server already accepts; there is no browser login.
- TLS verification stays on; use `--ca-file` for private CAs.
- 401/403 render as distinct guidance states; neither is retried
  automatically.
- Minimum RBAC for read-only use: `get`/`list`/`watch` on
  `workflows.argoproj.io/workflows` in the target namespace, plus the
  log/pod-read paths your Argo Server delegates to (namespace-scoped
  role is sufficient; no cluster-wide permissions needed).

## 7. Troubleshooting

| Symptom | Meaning / action |
|---|---|
| exits 1 with "real connections are not wired yet" | pre-alpha baseline binary: build from the integrated branch (I1) for real-connection support |
| `unauthenticated` state | token wrong/expired or auth-mode mismatch; check `tokenEnv`/`tokenFile`, no auto-retry by design |
| `forbidden` state | RBAC denial — authoritative; widen the namespace-scoped role or switch namespaces |
| stale banner with last good data | server unreachable; the app keeps showing the previous snapshot with its age and backs off with jitter |
| "workflow no longer available" | the workflow was deleted between list and open |
| logs unavailable with a cause | pod GC'd / logs not archived / node ID ≠ pod name — the message names the cause; use workflow-wide logs or enter a pod name |
| terminal size below 60×15 | resize notice; quit and help still work |

## 8. Known gaps in the current build (honesty)

- List/detail/logs views are composed by I1; this baseline renders the
  placeholder screens (the components exist and are tested per-component
  on their branches).
- `internal/app` 404-detail view gate: a not-found detail sets the
  correct internal state but the placeholder view still shows "loading"
  (fix queued for I1; recorded in [`testing.md`](testing.md) §7).
- Real-cluster compatibility is **unverified** until the ET-4 environment
  is provisioned (see [`testing.md`](testing.md) §4 and the acceptance
  matrix §6 ledger).

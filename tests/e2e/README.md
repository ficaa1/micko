# tests/e2e — REAL-cluster tier (ET-4/ET-5)

This package holds the tests that require an **explicitly provisioned,
disposable Argo Workflows environment** (kind cluster per
`docs/test-environment.md` §4, or an owner-authorized endpoint). It is
NOT part of the default `go test ./...` run:

```sh
# never runs by surprise: build tag e2e AND env flag AND allowlist file
ARGO_TUI_E2E=1 ARGO_TUI_E2E_CONFIG=./e2e-config.yaml \
  go test -tags=e2e ./tests/e2e/... -count=1 -v
```

## Gate (plan §9)

Three things are required, and the environment flag alone is **not
permission to touch a production cluster**:

1. Build tag `e2e` (keeps the suite out of ordinary `go test ./...`).
2. `ARGO_TUI_E2E=1`.
3. An allowlist file (`ARGO_TUI_E2E_CONFIG` or `./e2e-config.yaml`)
   carrying at least `server=` and `namespace=`. A commented example
   lives in [`e2e-config.example.yaml`](e2e-config.example.yaml).

When the gate is closed the tests **skip with the recorded cause** — an
explicit, attributed skip, never a silent pass (acceptance-matrix rule of
trust, `docs/acceptance-matrix.md` §1.1).

## Current honest state (E1, 2026-09-08)

| Item | State |
|---|---|
| Gate + allowlist parsing | Implemented (`gate.go`), verified: closed gate skips with cause; open gate without config skips with cause |
| Journey test | Implemented (`workflow_test.go`): submit → wait phase → **list via production Reader** → **detail via production Reader** → version identity (CMP-01/CMP-02) → delete + verify (cleanup contract §4.5) |
| Production adapter (internal/argo, A1 branch) | **Not merged on this branch yet** — the journey client skips with the `BLOCKED-DEPENDENCY` reason until I1 wires it (client.go dependency label; single flip point: `adapterMerged`) |
| ET-4 kind cluster on this host | **Not provisioned** — no docker/kind on the LXC (docs/acceptance-matrix.md §6, pre-registered BLOCKED-CAPABILITY; provisioning is a later task per test-environment.md §6) |

So today: `go test -tags=e2e ./tests/e2e/...` skips explicitly, both
without and with `ARGO_TUI_E2E=1` (missing allowlist). That is the
intended, labeled state — **not** a pass masquerade.

## Fixtures (synthetic; docs/test-environment.md §4.4)

| File | Shape | Purpose |
|---|---|---|
| `testdata/hello-world.yaml` | single container template | CMP-01 journey, list/detail |
| `testdata/steps-small.yaml` | two-step Steps template | DET-07 node-type fixture at real scale |
| `testdata/failing-step.yaml` | container exiting 1 | failed-node + log behavior |

All are labeled `argo-tui.e2e/owner=argo-tui-e2e` + `fixture: synthetic`,
use `generateName`, and are deleted by the journey's cleanup (t.Cleanup
plus the explicit delete/verify leg). They are authored for this suite —
never captured from a real cluster.

## Safety boundaries (⛨)

- The suite never mutates outside the allowlisted namespace; cleanup
  deletes only owner-labeled objects it created (§4.5).
- Credentials go in the allowlist's `token=` (or better: a temp SA token
  file referenced by a wrapper), never committed; fixtures carry no
  tokens.
- Writes here (submit/delete) exist ONLY in this REAL tier and only
  against the disposable allowlisted environment; the app itself remains
  read-only in v0.1 (plan §6).

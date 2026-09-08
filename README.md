# argo-tui

A read-only terminal UI for [Argo Workflows](https://github.com/argoproj/argo):
browse workflow lists, inspect details and stream logs — with the terminal
treated as hostile input territory (all untrusted text is sanitized before
rendering).

**Status: F1 foundation baseline.** The contracts, fake backend, config
validation and root model exist and are tested; the real REST adapter (A1)
and composed views (B1/C1/D1) are not wired yet. This is a compiling
baseline for downstream workers, **not a finished alpha**.

## Quick start

Requires Go ≥ 1.25 (module pins `go 1.25.0`, toolchain `go1.25.4`).

```sh
make build          # binary in dist/argo-tui
./dist/argo-tui --demo      # synthetic demo dataset (no network, no writes)
./dist/argo-tui --version
```

`--demo` is the only mode in the current baseline: it runs against the
in-memory fake Reader (never any I/O). Real connections are explicitly
refused with a hint until A1/I1 land — there is no network fallback.

## Development

```sh
make test        # required focused test set (F1 gate)
make test-race   # needs a C toolchain for the race runtime
make vet
make lint-fmt
make smoke
```

Layout (plan §4 ownership):

| Path | Owner | Contents |
|---|---|---|
| `cmd/argo-tui/` | F | entrypoint, `--demo` fake backend |
| `internal/core/` | F | frozen DTOs + `Reader` contract, typed errors |
| `internal/config/` | F | config precedence/validation (never leaks secrets) |
| `internal/app/` | F | root Tea model: routing, generations, cancellation |
| `internal/ui/shared/` | F | theme, keys, sanitizer/redactor |
| `internal/testkit/` | F | fake Reader, fake clock, synthetic fixtures |
| `internal/argo/` (A1) | A | REST adapter implementing `core.Reader` |
| `internal/ui/{workflowlist,detail,logs,actions}` | B/C/D/W | view components |

Docs: `docs/contracts.md` (frozen surface), `docs/protocol.md` +
`docs/adr/0001-architecture.md` (F0 verification), `docs/acceptance-matrix.md`
+ `docs/test-environment.md` (E0), and the project plan.

## Security posture

- Tokens come only from an env var **name** or token **file**; never
  literals in config, flags, or error messages. Sources are mutually
  exclusive and re-read per request (rotation).
- TLS verification on by default; custom CA supported; insecure mode needs
  an explicit flag and shows a permanent warning. Plain HTTP only to
  loopback. Redirects are rejected in the API client.
- Every untrusted string is sanitized (control sequences stripped) and
  token-shaped text redacted before rendering.
- v0.1 is strictly read-only: action intents are parsed but never
  converted to writes.

# Local changelog

## 0.2.0-beta.1 — local beta candidate

- Integrated guarded retry, resubmit, stop and terminate journeys with root-owned preflight, single-send mutation execution, read-back verification and explicit UNKNOWN outcomes for ambiguous results.
- Added protocol-shaped watch recovery for auth/permission terminal states, bounded 429 backoff, 410 relist, opaque cursors, stale-attempt rejection and JSON-lines/SSE framing.
- Added keyboard-safe terminate confirmation, cancel-default dialogs, sanitized action context and fail-closed disposable-environment E2E authorization gates.
- Independent final beta approval passed local functional, security, integration, race, vet, benchmark, build and checksum gates; live Argo compatibility remains BLOCKED without an authorized disposable v4.1.2 environment.

## 0.1.0-alpha — local handoff

- Integrated the read-only Argo Server REST adapter with workflow listing, pagination, detail/resource rendering and bounded log streaming.
- Added deterministic workflow/node views, terminal sanitization, stale/error states, profile/namespace configuration and offline synthetic demo mode.
- Added resilient watch and guarded action infrastructure for the beta wave.
- Built local Linux amd64, macOS amd64 and Windows amd64 binaries with `CGO_ENABLED=0`; artifacts remain in ignored `dist/` and were not published.
- Verified unit, race, vet, integration/PTY and focused benchmark commands on Linux amd64 with Go 1.25.4.
- Superseded locally by `0.2.0-beta.1`; the original Q2 findings and their disposition remain recorded in `docs/reviews/beta.md` and `docs/reviews/beta-approval.md`.

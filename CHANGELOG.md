# Local changelog

## 0.1.0-alpha — local handoff

- Integrated the read-only Argo Server REST adapter with workflow listing, pagination, detail/resource rendering and bounded log streaming.
- Added deterministic workflow/node views, terminal sanitization, stale/error states, profile/namespace configuration and offline synthetic demo mode.
- Added resilient watch and guarded action infrastructure for the beta wave.
- Built local Linux amd64, macOS amd64 and Windows amd64 binaries with `CGO_ENABLED=0`; artifacts remain in ignored `dist/` and were not published.
- Verified unit, race, vet, integration/PTY and focused benchmark commands on Linux amd64 with Go 1.25.4.
- Q2 beta review remains NOT APPROVED: confirmed UI actions currently have an intent-type wiring defect, watch auth/permission/rate-limit failures need terminal handling/backoff, and no real Argo compatibility environment was available. These limitations are recorded in `docs/reviews/beta.md` and `docs/compatibility.md`.

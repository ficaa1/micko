# Local changelog

## 0.2.0 — first real release

Verified against a live Argo Workflows v4.1.2 server, not only against
fixtures.

Added:

- Managed `kubectl port-forward`: argo-tui opens the connection to the Argo
  Server itself on a free local port and closes it on exit.
- Per-node logs. Argo does not publish pod names, so the pod name is derived
  with Argo's own algorithm from the `workflows.argoproj.io/pod-name-format`
  annotation. With no annotation the key stays inert instead of guessing.
- Suspended detection: a workflow holding a running `Suspend` node reads
  `◐ Suspended`, sorts to the top, is counted in the toolbar, and has its own
  phase filter.
- A readable node tree with tree connectors, fixed columns and a marked
  `AWAITING RESUME` gate row. Skipped subtrees are hidden by default.
- `f` full-screen view with no borders, `y` copy to the clipboard over OSC52,
  and `o` open in the Argo UI (needs `webURL` on the profile).
- Live search, page and `gg`/`G` movement in every view, and age-aware sorting
  inside each phase group.

Fixed:

- The poll timer was dropped whenever it fired off the list route, which
  stopped every refresh for the rest of the session.
- Scrolling above the first log line ate rows off the bottom of the pane.
- Paging to the top of a log desynchronised the scroll state from the screen,
  so the next arrow press moved nothing.
- Argo's pod-name hash is FNV-1a, not FNV-1. The first implementation
  requested pods that do not exist and returned empty logs.

Changed:

- `f` is the full-screen view on every route; `t` (tail) follows the log.
- Footers carry only the keys unique to each view. Movement lives in `?`.
- Actions stay off unless `--allow-actions` is passed.

## 0.2.0-beta.1 — live-smoke + review fixes (post-candidate, unreleased)

- Routed list keys (j/k/arrows/Enter/l//s/r/manual refresh) into the list child while keeping q and Ctrl-C global and isolating text entry during search.
- Propagated WindowSizeMsg to every child view model and redrew responsively on resize.
- Rendered the AGE column from the injected/current clock instead of the zero clock (which showed "-" on every row).
- Adopted Bubble Tea alternate-screen (full-window) behavior and restored the terminal on every exit/error path.
- Word-wrapped long server/error text at the terminal width instead of clipping it at the right edge.
- Integrated real-PTY regressions that pin the above against the compiled `--demo` binary.
- Fixed Esc-from-logs navigation (review finding): a single Esc now returns to the route the logs pane was opened from — the list when logs are opened with `l` on the list, or the loaded detail when logs are entered from detail — instead of landing on a never-loaded or stale detail pane.

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

# Changelog

## 0.3.1

- Added the profile picker. Started with no `--profile` and no `--server`,
  argo-tui opens it instead of connecting to whichever profile the config file
  names last, and `P` reopens it at any time. Switching profile is a full
  reconnection: every in-flight request is canceled, the snapshot is dropped
  and the port-forward is closed before the next one starts.
- Added an empty state to the profile picker: with no config file it shows the
  path to write, a profile that connects, and `--demo`.
- The workflow list's MESSAGE column now takes all the width the fixed columns
  leave, instead of a fixed share that stopped short of the pane edge.
- Licensed under the GNU General Public License version 3.
- CI now builds and tests on macOS as well as Linux, so the platform argo-tui
  is used on is the platform it is checked on.
- The module is now `github.com/ficaa1/argo-tui`, which is the path
  `go install` needs. The old path could never be fetched. While the
  repository is private no proxy can read either path.
- A binary built without an injected commit now reports the revision Go
  recorded, instead of "unknown". A build from a dirty tree still says
  "unknown", because it matches no commit.
- Releases now update the `ficaa1/homebrew-tap` formula from the checksums the
  release job produced, so the formula can never name a build the job did not
  verify. `brew install ficaa1/tap/argo-tui` downloads from the release, so it
  starts working when the repository becomes public.
- Added `docs/demo.tape` and the README recording it produces. `make demo`
  records it locally; the `demo` workflow records it on a runner with a pinned
  vhs toolchain, which is the path that needs nothing installed.

## 0.2.1

- Fixed the detail view never refreshing on its own: a superseded reply left
  it marked as loading, so only `r` showed a new phase.
- Fixed a late reply retiring the cancel function of the request that
  replaced it, which left that request running with nothing able to stop it.
- Fixed a real run reading the demo's frozen clock, which held every age and
  stale timer at the moment of start.
- Fixed an unbounded second copy of every log line held beside the bounded
  buffer, and a watch that added workflows past the snapshot cap.
- Fixed a data race on the watch request, and an unavailable managed endpoint
  falling back to a port the operating system had already released.
- Added a deadline to every non-streaming request, so a stalled response body
  can no longer hold the refresh open. Log and watch streams keep none.
- Fixed action outcomes claiming more than was observed: a check that failed
  before anything was sent now reports REFUSED, and `confirmed` requires the
  expected end state.
- Added sorting to the nodes tab with `s`, and stopped hiding running nodes
  under a skipped parent.
- Fixed log tailing, which never asked the server to follow the stream.
- Fixed the end-to-end and connected test suites, neither of which could fail.

## 0.2.0

- Added managed `kubectl port-forward`, per-node logs and suspended-workflow
  detection with waiting approval gates in the node tree.
- Added borderless full-screen views, clipboard copy, browser links, live
  search and paging throughout the UI.
- Fixed polling after leaving the list, log scrolling and Argo pod-name hashing.
- Standardized `f` for full screen and `t` for following logs. Actions remain
  opt-in with `--allow-actions`.
- Recorded live use on macOS arm64 with Argo Workflows v4.1.2: list/watch,
  node logs, Resume, Stop and managed-forward cleanup.

## 0.2.0-beta.1

- Added watch recovery and guarded actions with identity checks, single-send
  execution, read-back verification and explicit unknown outcomes.
- Fixed keyboard routing, resize handling, workflow ages, terminal restoration
  and navigation back from logs.
- Added PTY regressions and opt-in E2E authorization gates.

## 0.1.0-alpha

- Added workflow listing, detail/resource views, bounded log streaming,
  profile configuration and an offline demo.
- Added terminal sanitization and explicit loading, stale and error states.

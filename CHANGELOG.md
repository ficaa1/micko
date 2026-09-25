# Changelog

## Unreleased

- Parameter and output values are now shown by default. The redaction they
  used to open with hid what a reader opens a workflow to see. A cluster
  whose parameters carry secrets can turn it back on with `redactValues: true`
  at the top of the config file or on a profile, or with `--redact-values`;
  `v` still hides or reveals them for the session.
- The Nodes tab reads like the pipeline it shows. Steps are listed under
  their Steps node in group order instead of as a staircase of step groups;
  DAG tasks are listed in dependency order with what each waits for
  (`← extract`), so a join appears once; retry attempts nest under their
  Retry node; and the exit handler is its own labelled tree. The default
  `s` order follows the pipeline, and start time, name and phase remain.
- Each node row now carries its run: the template on a wide terminal, the
  duration (elapsed while it runs), a timing bar that places it on the
  workflow's clock, and the message. Structural nodes are tagged with their
  type, a Retry node shows its retry count and a failing pod its exit code.
  Above the tree, a progress line shows the workflow's progress with a bar,
  a count of the work by state, and the elapsed time against the estimate.
- Subtrees fold: `space` folds or unfolds, `left` folds or climbs to the
  parent, `right` unfolds. Folds survive refreshes, and earlier retry
  attempts start folded.
- `i` opens a node info panel with everything the workflow says about the
  node under the cursor: times, pod and host, exit code, resource usage,
  flags, and inputs and outputs, with parameter values that `v` hides or reveals.
  It sits to the right on a wide terminal and under the tree otherwise.
- `/` finds a node by name; `n` and `N` step through the matches, opening
  folds on the way. While the find input is open every letter types, `q`
  included, and `esc` clears the find before it leaves the workflow.
- Added skins. `default` keeps the terminal's own 16-colour palette; the
  truecolor skins are catppuccin-mocha, catppuccin-latte, gruvbox-dark,
  gruvbox-light, nord, dracula, tokyo-night, solarized-dark, solarized-light,
  one-dark, rose-pine, rose-pine-dawn and monokai; `auto` picks a dark or
  light skin from the terminal's background colour. Choose one with `skin:`
  in the config file, per profile, or with `--skin`, which also works in the
  demo. An unknown name stops the program at startup and lists the valid
  ones. `NO_COLOR` still gives plain text.
- The screen chrome is drawn with more care. The header band shows the
  program, server and namespace in separate styles and the safety mode as a
  badge, louder when actions are enabled. Footer keys stand out from their
  descriptions. The selected row is a bar across the pane, the list colours
  only the phase cell and mutes the times and message, table heads and tree
  connectors are styled, and the detail tabs, pickers, action pane, help
  overlay and log annotations use the same palette. Truecolor skins round
  the border corners. The text on screen is unchanged: every phase keeps its
  glyph and word.

- The demo dataset now has twelve workflows whose node maps follow the
  shapes the Argo controller writes: chained step groups, DAG tasks that list
  their dependents as children, retry attempts, an exit handler, a fan-out in
  progress, an approval gate, an out-of-memory kill, a validation error with
  no nodes, a deploy tree that is mostly skipped branches, and runs started by
  a CronWorkflow. Every pod that ran has its own log, and the resource tab
  shows a full manifest.
- The adapter now decodes each node's progress, duration estimate, resource
  usage, host, exit code, input and output parameters and artifacts, and its
  retry, hook and memoization flags, and each workflow's progress and duration
  estimate. The list asks for the workflow's progress and estimate too.
- A port-forward that prints an error line for one dropped connection no
  longer counts as a lost connection. kubectl prints such a line while the
  listener keeps serving, and it blocked every action until the process
  itself restarted.
- A stale snapshot is now recollected off the list route. Actions stay
  blocked until a fresh list is accepted, and the poll collected one on the
  list route alone, so the block never cleared while a workflow was open.
- The workflow list now asks the server for the fields a summary row needs
  instead of the whole workflow object. The spec, the stored templates and
  the node maps dominate the payload, and every poll downloaded them for
  every workflow. The node maps are fetched by a second narrow request for
  the workflows that have not completed, which is the only set that can hold
  a running Suspend node, so the list keeps its gate marker.

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

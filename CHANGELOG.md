# Changelog

## Unreleased

- Parameter and output values are now shown by default. The redaction they
  used to open with hid what a reader opens a workflow to see. A cluster
  whose parameters carry secrets can turn it back on with `redactValues: true`
  at the top of the config file or on a profile, or with `--redact-values`;
  `v` still hides or reveals them for the session.
- Workflow-wide logs now label each line with the step that wrote it, or the
  pod when the step is not known, coloured per source. `L` turns the labels
  off and on.
- `w` in the log pane wraps long lines and keeps the line you were reading in
  place. Search highlights carry across the wrapped lines.
- A log line that starts with a level word such as `ERROR`, `WARN` or
  `DEBUG`, after any timestamps, or a JSON line with a `level` or `severity`
  field, has that word coloured. The rest of the line is untouched.
- `&` in the log pane shows only the lines matching the `/` search, and says
  how many of the retained lines that is. `&` again or `esc` shows every line.
- `ctrl+t` in the log pane reopens the stream with server timestamps on or
  off. The retained lines stay, and a marker shows where the new stream
  starts.

- The list filter now reads a small query language. Spaces separate terms
  that must all match, `|` offers alternatives, `!` negates, `/.../` is a
  regular expression and `~` a fuzzy match. `phase=`, `age<`, `dur>`,
  `label:`, `tmpl=` and `cron=` filter on the phase, the age, the run time,
  the labels and the template or CronWorkflow a run came from. A plain word
  still matches names. A term that does not parse leaves the filter as it
  was and says why in the toolbar, which shows the applied filter as it was
  read.
- `w` on the workflow list adds wide columns: progress with a bar, start and
  finish times, the template, the CronWorkflow and the remaining labels. They
  give way in a fixed order as the pane narrows. A pane of 120 columns or
  more shows the progress column without `w`.
- The space bar now types a space in the list filter instead of being
  dropped.

- Added marks and bulk actions. `space` marks the selected workflow and `esc`
  clears the marks before it clears the filter. Marks follow their workflow
  through refreshes, sorting and filtering, and the toolbar counts them,
  including the ones the filter hides. With marks, `a` acts on every marked
  workflow: the menu says how many each verb applies to, the confirmation
  lists them, and the requests go out one at a time, each with its own
  identity check and read-back and none ever resent. The result lists every
  workflow's outcome.
- `a` now works on the workflow list too, for the selected workflow, behind
  the same fresh-data gate as the detail view.
- Added suspend (`z`), terminate (`t`) and delete (`d`) to the actions menu.
  Terminate asks for the workflow's name, or for the number of workflows in a
  bulk action. Delete asks twice: a final screen deletes only on `D`.
- The actions menu now offers only the verbs that apply to the workflow's
  phase, and an action whose workflow has moved on since the menu opened is
  refused before anything is sent.
- A workflow suspended with `spec.suspend` now reads as Suspended in the list,
  and resume is offered for it.
- Every write attempt is now recorded in
  `~/.local/state/argo-tui/actions.jsonl` (or under `$XDG_STATE_HOME`), one
  JSON line per attempt. `journal: false` in the config file turns it off.

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

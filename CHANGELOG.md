# Changelog

## Unreleased

- Parameter and output values are now shown by default. The redaction they
  used to open with hid what a reader opens a workflow to see. A cluster
  whose parameters carry secrets can turn it back on with `redactValues: true`
  at the top of the config file or on a profile, or with `--redact-values`;
  `v` still hides or reveals them for the session.
- A Timeline section, after Nodes, draws the workflow as a Gantt chart: pods
  and approval gates as bars coloured by phase on a time axis whose ticks
  step in seconds, minutes, hours or days as the run requires, with a `now`
  line while the workflow runs. Groups are brackets over the time they took,
  shading marks the time a step waited before it started, and `◆` marks the
  critical path, the chain of work that set the end time. Rows follow the
  pipeline order and folds of the Nodes tab, `i` shows the node info panel
  and `enter` opens the pod's log.
- `1` to `9` jump to a detail section by its position, and `T` jumps to the
  Timeline. `T` on the workflow list opens the selected workflow straight on
  its Timeline.
- The help overlay is tighter, so it fits a 40-row, 80-column terminal whole.
- An Explain section, after Timeline, says why a workflow ended the way it
  did, from what the workflow records and nothing else: no network service,
  no model, the same answer every time. Each finding is a card with a
  severity, a headline, its evidence and a next step. It finds the node
  that failed first on its own, explains exhausted retries, out-of-memory
  kills, well-known exit codes, image pull errors and pods that never
  started, deadlines and rejected specs, lists what did not run because of
  the failure, and reports the exit handler, a gate waiting for a person, a
  run past its estimate, a workflow the controller has not started, and
  steps that needed a retry in a run that succeeded. For a failed pod it
  reads the end of the log and quotes the lines that matter; a log that is
  gone is said so. `y` copies the explanation as text for an incident
  channel.
- `X` jumps to the Explain section in the detail pane, and `X` on the
  workflow list opens the selected workflow straight on it.
- An Events section, after Explain, streams the Kubernetes events about the
  workflow and its pods while it is open: age, type (as a glyph and a word),
  reason, the workflow or the node, count and message. `s` puts warnings
  first and `/` filters. A dropped stream reconnects with back-off, and a
  permission error or a server without Argo's event stream is said on the
  status line. `E` jumps to it in the detail pane and opens the selected
  workflow on it from the list. The demo serves synthetic events.
- The detail tab strip closes up, and then shows the tabs around the active
  one, when the terminal is too narrow for all of them.

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
- Added a command palette. `:` opens it on every route; typing ranks the
  commands and their aliases, `tab` completes the highlighted one, and `enter`
  runs exactly what was typed. `wf` shows the workflow list, `ns [name]` and
  `profile [name]` (or `ctx`) switch namespace and profile or open their
  pickers, `all` toggles all namespaces, and `help` and `q` do what their keys
  do. Namespace and profile names complete after a space, and `ctrl+p` and
  `ctrl+n` recall earlier commands. A word that names no command is reported
  in the footer, never run as a near match.
- Added an all-namespaces view: `0` on the workflow list, or `:all`. The list
  shows every workflow the token may read with a NAMESPACE column, the header
  reads `ns: all`, the filter also matches `namespace/name`, and detail, logs
  and actions use each row's own namespace. A token that may not list
  cluster-wide, or a server started for one managed namespace, gets that
  reason on the pane instead of an empty list.
- Added a cron workflow list: `:cron` (or `:cwf`, `:cronworkflows`). It
  shows each CronWorkflow's schedules, time zone, suspend state, active runs,
  last run, next run and concurrency policy, soonest next run first and
  suspended ones last, with `n` and `0` for the namespace as on the workflow
  list. Next run times are computed from the schedule the way the controller
  reads it, in the object's time zone and across daylight-saving changes; a
  schedule the controller would refuse shows `?` with the reason. `i` opens
  an info panel with the next five runs, the policy, the history limits, the
  last and active runs and the arguments, whose values `v` hides or reveals. `enter`
  lists the workflows the cron workflow started, and `esc` returns. Both the
  v3.5 `schedule` field and the v3.6+ `schedules` list are read. The demo has
  four cron workflows, one of them owning the demo's hourly ETL runs.
- Added workflow template and cluster workflow template lists: `:tmpl` (or
  `:wftmpl`, `:workflowtemplates`) and `:cwftmpl` (or
  `:clusterworkflowtemplates`). They show each template's entrypoint, how many
  templates and parameters it defines, its age and its description. `i` opens
  an info panel with the arguments (values follow `redactValues`, with their
  defaults and allowed values), each template and its type, the service
  account and the labels, and `enter` lists the workflows submitted from the
  template. Cluster templates belong to no namespace, so `n` and `0` say so
  instead of switching. The demo has the templates its workflows name, and a
  cluster template its hello-world run came from.
- Added an archived workflow list: `:aw` (or `:archived`). It lists the
  workflow archive with the workflow list's columns, the newest 300 runs, and
  `enter` opens a run in the detail pane from the archive, marked as archived.
  Archived runs are not refreshed and cannot be acted on, and their log pane
  says when nothing came back because the pods are gone. A server without an
  archive gets "the workflow archive is not enabled on this server" instead of
  an error.
- A list that fails before anything was collected now says so on the pane
  instead of reading as a namespace with no workflows.
- The demo has a second namespace, `demo-ml`, with a hyperparameter sweep in
  progress and a finished batch inference run.
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
  still matches names, and in the all-namespaces view a word, a regular
  expression or a fuzzy pattern also matches `namespace/name`. A term that does not parse leaves the filter as it
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

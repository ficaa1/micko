# Changelog

## Unreleased

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
  last and active runs and the arguments, whose values `v` reveals. `enter`
  lists the workflows the cron workflow started, and `esc` returns. Both the
  v3.5 `schedule` field and the v3.6+ `schedules` list are read. The demo has
  four cron workflows, one of them owning the demo's hourly ETL runs.
- Added workflow template and cluster workflow template lists: `:tmpl` (or
  `:wftmpl`, `:workflowtemplates`) and `:cwftmpl` (or
  `:clusterworkflowtemplates`). They show each template's entrypoint, how many
  templates and parameters it defines, its age and its description. `i` opens
  an info panel with the arguments (values redacted until `v`, with their
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

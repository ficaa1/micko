# micko

```text
    .---.                _        _
   ( o o )    _ __ ___  (_)  ___ | | __  ___
  ((  V  ))  | '_ ` _ \ | | / __|| |/ / / _ \
   (     )   | | | | | || || (__ |   < | (_) |
  ~~"~~~"~~  |_| |_| |_||_| \___||_|\_\ \___/
```

micko is a keyboard-first terminal UI for [Argo Workflows](https://argoproj.github.io/workflows/).
Browse workflows, inspect nodes and resources, stream logs, and resume, retry,
resubmit or stop a run with explicit confirmation. It was called argo-tui
until 0.5.0.

It is named after Mićko, my eastern rosella, who is also its mascot. Turn him
on with `mascot: true` in the config file, `--mascot`, or `:mascot` at any
time, and on a terminal of 80×40 or larger he perches on top of the pane, the
way he rests his beak on a monitor. He is off by default: he costs the pane
three rows.

![micko browsing the demo dataset](docs/demo.gif)

## Build and try it

Requires Go 1.25 or later; the module selects Go 1.25.4 as its toolchain.

```sh
git clone https://github.com/ficaa1/micko.git
cd micko
make build
./dist/micko --demo
```

Without Make, use `go build -o dist/micko ./cmd/micko`. `make build`
also stamps the Git commit into the binary. `--version` prints both version
and commit. The demo uses synthetic data without connecting to a cluster or
reading credentials. Press `?` for help and `q` to quit.

## Install

The repository is private, so `go install` and Homebrew cannot fetch it: both
need anonymous access. The release job still builds the binaries, the
checksums and the tap formula, so both paths start working on the day the
repository becomes public.

Until then, download a binary from
[the releases page](https://github.com/ficaa1/micko/releases) while signed
in, verify it and put it on your PATH:

```sh
VERSION=0.6.0   # the release you want
gh release download "v$VERSION" --repo ficaa1/micko \
  --pattern '*_darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing   # sha256sum -c on Linux
tar -xzf "micko_${VERSION}_darwin_arm64.tar.gz"
```

Releases up to 0.5.0 were published as `argo-tui_<version>_<os>_<arch>.tar.gz`,
with a binary named `argo-tui`.

## Connect

Copy [the example config](docs/micko.config.example.yaml) to
`~/.config/micko/config.yaml` and edit it for your cluster. A minimal
profile for managed forwarding is:

```yaml
currentProfile: dev
profiles:
  dev:
    kubeContext: my-cluster
    service: argo-workflows-server
    serviceNamespace: argo
    remotePort: 2746
    server: http://127.0.0.1:2746
    namespace: workflows
    tokenEnv: MICKO_TOKEN
```

Start it with no arguments and it opens the profile picker:

```sh
./dist/micko
```

Or name the profile to connect to it directly:

```sh
./dist/micko --profile dev
```

Managed forwarding requires `kubectl` on PATH and access through the named
Kubernetes context. micko starts its own forward on a free loopback port
and closes it on exit. The profile's `server` supplies the scheme and path
prefix; the forward supplies the host and port. Use HTTPS if the Argo Server
expects TLS. For an already reachable endpoint, omit `kubeContext`, `service`,
`serviceNamespace` and `remotePort`; `server` is then used directly.

Configuration lookup checks `$XDG_CONFIG_HOME/micko/config.yaml`, then
`~/.config/micko/config.yaml`, then the OS user config directory, using
the first existing file. `--config PATH` selects a file explicitly.
With no `--profile` and no `--server`, micko opens the profile picker and
connects to nothing until you choose. `currentProfile` places the cursor on a
row; it does not connect by itself. `P` reopens the picker at any time, and
switching profile is a full reconnection: in-flight requests are canceled, the
snapshot is dropped and the port-forward is closed before the next one starts.
With no config file the picker shows the path to write and a sample profile.

### Authentication and TLS

Configure exactly one of `tokenEnv` (an environment variable name) or
`tokenFile` (an absolute file path). The source is read for each request.
The current config validator requires a source even when no token is needed:
for Argo's server auth mode, keep `tokenEnv: MICKO_TOKEN` and leave that
variable unset or empty. An empty token sends no Authorization header.
For client auth, supply an accepted bearer token through that source;
the Kubernetes context does not provide an Argo token automatically.
There is no interactive SSO login.

HTTPS verifies certificates by default; `--ca-file` supplies a custom CA.
`--insecure-skip-tls-verify` disables verification for that invocation and
shows a warning. Plain HTTP is limited to loopback addresses. Redirects are
rejected. Never put literal tokens in command arguments or committed config.

### Actions

Actions are disabled by default. Enable them for a session with:

```sh
./dist/micko --profile dev --allow-actions
```

Press `a` on the workflow list or in a workflow's detail view. The menu offers
only the verbs that apply to the workflow's phase:

| Key | Verb | Applies to |
| --- | --- | --- |
| `u` | resume | a suspended workflow |
| `z` | suspend | a running workflow that is not suspended |
| `r` | retry | a failed or errored workflow |
| `b` | resubmit | a finished workflow |
| `s` | stop | a running workflow; graceful, runs exit handlers |
| `t` | terminate | a running workflow; no exit handlers |
| `d` | delete | any workflow |

The confirmation identifies the target; only `y` confirms, and Enter and Esc
cancel. Terminate asks you to type the workflow's name. Delete cannot be undone,
so it asks twice: after `y`, a final screen deletes only on `D` (shift+d) and
any other key cancels. Neither the `d` that opened it nor the `y` that passed
the first step can finish it, however often it is pressed.

Before sending, the app reads the workflow again and refuses the action when
its identity changed or the verb no longer applies. It sends exactly one request
and never retries a write. The outcome is `CONFIRMED` when the expected end
state was observed (resumed, suspended, left its failed phase, reached a
terminal phase, gone), `ACCEPTED` when the server applied it but the end state
was not seen within five seconds, `REFUSED` when nothing was sent, and
`UNKNOWN` when the server may or may not have applied it; inspect the workflow
before deciding what to do next. The server's permissions remain authoritative.
Argo's action endpoints address workflows by name and have no UID precondition,
so a same-name replacement between the identity check and the write remains
possible. With the workflow archive enabled, a deleted workflow can still be
read from the archive, so its delete reports `ACCEPTED` rather than `CONFIRMED`.

#### Marks and bulk actions

`space` marks or unmarks the selected row; a marked row carries a `◆` in its
first column and the toolbar counts the marks. Marks follow their workflow
through refreshes, sorting and filtering. A mark hidden by the filter still
counts, and the toolbar says how many are hidden. A mark whose workflow is gone
from the snapshot is dropped, and switching namespace or profile clears them
all. `esc` clears the marks first and the filter on the next press.

With marks, `a` acts on every marked workflow, hidden ones included. The menu
says how many marked workflows each verb applies to, and the confirmation lists
them; workflows the verb does not apply to are left alone. A bulk terminate asks
you to type the number of workflows instead of a name. The requests are sent
one at a time, each with its own identity check and read-back, and none is ever
resent. A workflow that fails its check is refused without affecting the
others. `esc` during the run stops it after the request in flight; losing the
connection or the fresh snapshot stops it too. The result lists every workflow's
outcome with a total, and stays on screen until you close it. Closing it clears
the marks and refreshes the list.

#### Action journal

Every write attempt, refused ones included, appends one JSON line to
`$XDG_STATE_HOME/micko/actions.jsonl` (by default
`~/.local/state/micko/actions.jsonl`): time, profile, server, namespace,
name, UID, verb, outcome and error text. The file is created readable by you
only. A journal that cannot be written never blocks or changes an action; the
footer reports the first failure of the session. Sessions without
`--allow-actions` and the demo write nothing. To turn the journal off, set
`journal: false` at the top level of the config file.

## Everyday keys

| View | Keys |
| --- | --- |
| Navigation | `j`/`k` or arrows; `pgup`/`pgdn`; `gg`/`G` or `home`/`end` |
| Workflow list | `enter` open, `T` open on its timeline, `X` open on its explanation, `E` open on its events, `l` workflow logs, `/` filter, `s` sort, `p` phase, `n` namespace, `0` all namespaces, `space` mark, `a` actions, `w` wide columns, `esc` clear marks then filter |
| Cron workflows (`:cron`) | `enter` the row's workflows, `i` info panel, `v` hide or reveal values, `/` search, `s` sort, `n` namespace, `0` all namespaces, `f` manifest |
| Templates (`:tmpl`, `:cwftmpl`) | the same keys; `n` and `0` do not apply to cluster templates |
| Archived workflows (`:aw`) | `enter` open the run, `i` info panel, `/` search, `s` sort, `n` namespace, `0` all namespaces, `f` record |
| Detail | `tab`/`shift+tab` switch Summary, Nodes, Timeline, Explain, Events and Resource; `1`–`9` jump to a section by position; `T` timeline; `X` explain; `E` events; `r` refresh; `a` actions |
| Actions | `u` resume, `z` suspend, `r` retry, `b` resubmit, `s` stop, `t` terminate, `d` delete; `y` confirms, `D` finishes a delete |
| Nodes | `enter`/`l` selected node's logs, `space` fold/unfold, `left`/`right` fold or climb/unfold, `i` node info, `/` find by name, `n`/`N` next/previous match, `h` show skipped nodes, `s` sort, `p` phase filter |
| Timeline | `enter`/`l` selected node's logs, `space` fold/unfold, `left`/`right` fold or climb/unfold, `i` node info |
| Explain | `y` copy the report, `l` the failing pod's full log, `v` hides or reveals parameter values |
| Events | `s` warnings first or newest first, `/` filter, `y` copy the table, `r` refresh and restart the stream |
| Resource | `v` hides or reveals parameter/output values (also in the node info panel) |
| Logs | `t` follow tail, `space` pause scrolling, `c` container, `/` search, `n`/`N` matches, `&` only matching lines, `w` wrap, `L` source labels, `ctrl+t` server timestamps, `\|` pipe |
| Display | `f` borderless full screen, `y` copy, `o` open workflow in Argo UI |
| Command palette | `:` open, `tab` complete, `up`/`down` choose, `enter` run, `ctrl+p`/`ctrl+n` history, `esc` close |
| General | `P` switch profile, `?` help, `esc` back/cancel, `q` quit outside text entry, `ctrl+c` quit globally |

### Filtering and wide columns

`/` filters the list as you type. A plain word matches workflow names; the
rest is a small query language:

| Query | Matches |
| --- | --- |
| `etl report` | names containing `etl` and `report` (spaces separate terms; every term must match) |
| `etl\|report` | names containing either (`\|` joins alternatives within a term) |
| `!etl` | names not containing `etl`; `!` negates one alternative, so `!a\|b` is "not a, or b" |
| `/^demo-.*-\d+$/` | names matching a Go regular expression, case-insensitive |
| `~ddp` | names containing `d`, `d`, `p` in that order (fuzzy) |
| `phase=failed`, `phase!=succeeded` | the phase, case-insensitive; `suspended` is a phase here, and `running` includes it |
| `age<2h`, `age>1d` | time since the workflow started, or was created if it has not |
| `dur>10m` | run time; a running workflow counts until now |
| `label:team=data`, `label:team`, `label:!team` | a label's exact value, its presence, its absence |
| `tmpl=nightly` | started from this WorkflowTemplate or ClusterWorkflowTemplate |
| `cron=etl-hourly` | started by this CronWorkflow |

Durations take `s`, `m`, `h` and `d`, combined as in `1d12h`. A workflow with
no timestamp matches neither side of an `age` or `dur` bound. A term that does
not parse leaves the previous filter applied and shows its error in the
toolbar, and `enter` will not apply it; `esc` cancels the edit. Once applied,
the toolbar shows the filter as it was read, such as `phase=Failed & age<2h`.
Like the rest of the list, the filter runs on the collected snapshot, not on
the server.

`w` toggles wide columns: PROGRESS (the server's done/total count of pods, with
a bar), STARTED and FINISHED in local time, TEMPLATE, CRON and LABELS (the
labels no other column shows). As the pane narrows they drop in this order:
LABELS, CRON, FINISHED, MESSAGE, TEMPLATE, STARTED, PROGRESS. Without `w`, a
pane of 120 columns or more shows PROGRESS as well.

### Reading logs

A workflow's logs mix every pod, so each line starts with a label naming the
step it came from, or the pod when the step is not known yet, coloured per
source. `L` hides or shows the labels; a single pod's log starts without them.
`w` wraps long lines, keeping your place: the line at the bottom of the pane
stays there. A line whose first word after its timestamps is `ERROR`, `WARN`,
`DEBUG` or a similar level, or a JSON line whose `level` or `severity` field
names one, gets that word coloured; the rest of the line is left alone.

`&` shows only the lines matching the current `/` search, as in `less`; `&`
again or `esc` shows everything. The status line says how many lines are
shown out of how many are retained. Collection carries on underneath and the
retention limits are unchanged.

`ctrl+t` asks the server to put its own timestamp in front of every line. The
stream is reopened with the new setting, which replays the log from the start;
the lines already on screen stay, and a marker shows where the new stream
begins. `t` (follow) and `T` (the timeline elsewhere) were taken, so the
timestamp toggle is `t` with control.

Suspended workflows sort first by default and their waiting nodes are marked
`AWAITING RESUME`. Node logs require a known pod name: the adapter uses the
workflow's pod naming annotation and leaves the shortcut unavailable when
it cannot resolve one safely.

`:` opens the command palette on every route. Type a command and the palette
lists the matches with their aliases and a one-line description, best first;
`tab` completes the highlighted one and `enter` runs what is typed. Enter never
runs a guess: a word that names no command is reported in the footer.

| Command | Does |
| --- | --- |
| `workflows`, `wf` | Show the workflow list |
| `cronworkflows`, `cwf`, `cron` | Show the cron workflow list |
| `workflowtemplates`, `wftmpl`, `tmpl` | Show the workflow template list |
| `clusterworkflowtemplates`, `cwftmpl` | Show the cluster workflow template list |
| `archived`, `aw` | Show the archived workflow list |
| `ns [namespace]` | Switch namespace; with no name, open the namespace picker |
| `all` | Toggle the all-namespaces view |
| `profile [name]`, `ctx [name]` | Switch profile; with no name, open the profile picker |
| `mascot` | Toggle Mićko, the mascot |
| `help` | Show every key |
| `quit`, `q` | Quit |

After `ns` and `profile` a space starts completing the argument from the
namespaces and profiles the session knows. `ctrl+p` and `ctrl+n` step through
the commands run earlier in the session.

`0` on the workflow list, or `:all`, lists every namespace the token may read.
The header shows the namespace as `all`, the list gains a NAMESPACE column,
and the `/` filter matches `namespace/name`, so `team-a/` narrows it to one
namespace. Detail, logs and actions use each row's own namespace. The token
needs permission to list workflows cluster-wide; without it, and on a server
started for one managed namespace, the pane says so, and `0` returns to the
namespace you came from.

### Node view

The Nodes tab draws the workflow the way it runs, not the way the controller
records it. A steps template lists its steps under the Steps node in group
order, without the step-group bookkeeping between them. A DAG lists its tasks
in dependency order and notes what each one waits for (`← extract`), so a
task that joins six others is one row. Retry attempts sit under their Retry
node, loop items under their task group, and the exit handler is a tree of
its own, labelled as such.

Above the tree, a progress line gives the server's `done/total` with a bar,
a count of the work by state (`✓ 7  ● 3  ○ 1  ✗ 1`), and the elapsed time
with the controller's estimate when it has one (`elapsed 4m12s of ~9m`).
Each row shows the phase glyph and name, then, as the terminal allows, the
template (140 columns and up), the duration (elapsed while running), a timing
bar that places the node on the workflow's clock (80 and up), and the
message. Structural nodes carry a short type tag, a Retry node its retry
count (`↻ 2`), and a failing pod its exit code.

`space` folds or unfolds the subtree under the cursor; `left` folds, or
climbs to the parent from a folded row or a leaf, and `right` unfolds. A
folded row shows `▸` and how many rows it hides. Folds survive refreshes.
Earlier retry attempts that have a subtree start folded, since the last
attempt is the one that decided the outcome.

`i` opens the node info panel: names and ID, type and template, phase and
message, times, duration and estimate, pod, host, exit code, resource usage,
flags, and inputs and outputs. It sits on the right of a wide terminal and
under the tree otherwise. Parameter values and the script result follow the
Resource tab: shown, or hidden under `redactValues`, and `v` flips them. Everything in it
comes from the workflow already loaded.

`/` finds a node by name, ignoring case; `enter` jumps to the first match,
opening folds on the way, and `n`/`N` step through the rest. The status line
says what the tab is not showing: skipped rows, rows in folds, the phase
filter and the match.

### Timeline

The Timeline section (`T` in the detail pane, or `T` on the list to open a
workflow straight onto it) draws the workflow's work as a Gantt chart against
a time axis. Pods and approval gates are bars, coloured by phase, placed
exactly as the Nodes tab's timing bars place them; DAG, Steps and Retry nodes
group them as a bracket over the time the group took. The axis picks its
step from the span, from seconds for a quick run to hours or days for a long
one, and a running workflow's chart ends at a `now` line that its running
bars reach.

A shaded stretch (`░`) before a bar is time the node spent waiting: from the
moment what it waited for finished (the previous step group, its DAG
dependencies, the previous retry attempt) to the moment it started. A retry's
backoff and a queue for a free slot both show up there.

Nodes marked `◆` are the critical path: the chain of work that set the end
time, found by walking back from the work that finished last through
whatever each piece waited for longest. Shortening anything else would not
have finished the run sooner. While a workflow runs, the chain is the one its
end waits on so far.

Rows follow the Nodes tab's pipeline order and share its folds: `space`,
`left` and `right` fold groups the same way, `i` opens the same info panel,
and `enter` or `l` opens the selected pod's log. Skipped branches have no
time to place and are left out; the status line counts them.

### Explain

The Explain section (`X` in the detail pane, or `X` on the list to open a
workflow straight onto it) says why the workflow ended the way it did. It
applies fixed rules to what the workflow records and to the end of the
failing pod's log; nothing leaves the terminal and no model is asked, so the
same workflow always gets the same explanation.

Each finding is a card: a severity (`✗ ERROR`, `▲ WARNING`, `◇ INFO`, as a
glyph and a word), a headline, the evidence it rests on, and a next step.
The rules find the node that failed first on its own, rather than the DAG or
Steps nodes that failed because of it or the steps that failed after it;
the attempts of an exhausted retry and whether they failed the same way; an
out-of-memory kill; what exit codes 1, 2, 126, 127, 137, 139 and 143 mean;
image pull errors and other reasons a pod never started; a deadline; a spec
the controller rejected before any node ran; the nodes that did not run
because of the failure; how the exit handler went; a gate waiting for a
person, and for how long; a run past its estimate; a workflow the controller
has not started; and, for a run that succeeded, any step that needed a retry
or failed without stopping it.

When a pod failed, the section reads the last 200 lines of its `main`
container's log while it is open and quotes up to eight of them: lines with
an error word, whole tracebacks, and the line before each for context. The
status line says while it reads. A log the server no longer has is a
finding of its own. `y` copies the whole explanation as plain text, ready to
paste into an incident channel, and `l` opens the failing pod's full log.
Parameter values in the evidence follow the same reveal setting as the
Resource tab.

### Events

The Events section (`E` in the detail pane, or `E` on the list) streams the
Kubernetes events about the workflow and its pods while it is open: the
controller's `WorkflowRunning`, `WorkflowNodeFailed` and `WorkflowFailed`,
and the scheduler's and kubelet's `Scheduled`, `Pulled`, `BackOff`,
`FailedScheduling` and the like. Each row gives the time since it was last
seen, the type as a glyph and a word (`▲ Warning`, `◇ Normal`), the reason,
the object (`workflow`, or the node's name for a pod), the count and the
message. `s` puts warnings first, `/` filters as you type, `y` copies the
table and `f` shows it full screen.

It reads Argo's event stream (`/api/v1/stream/events/{namespace}`) twice:
once for the workflow's own events and once for the namespace's pod events,
which are matched to the workflow's pods by name, because Kubernetes cannot
select a workflow and its pods in one query. A dropped stream reconnects
with back-off from where it stopped; a permission error or a server without
the stream stops it with the reason on the status line, and `r` tries again.
The streams close when you leave the section or the workflow. Kubernetes
keeps events for about an hour by default, so a workflow that finished
earlier may have none. The account behind the token needs permission to
watch events in the namespace.

### Cron workflows

`:cron` lists the namespace's CronWorkflows: the schedule (every entry of a
v3.6+ `schedules` list, or the single `schedule` of older objects), the time
zone when it is not UTC, whether it is suspended, how many runs are active,
when it last ran, when it runs next, and its concurrency policy. The default
order puts the next run first and suspended ones last; `s` also sorts by name
and by last run.

NEXT RUN is computed locally from the schedule the way the Argo controller
reads it: five fields with ranges, steps, lists and month and weekday names,
the `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly` and `@every`
descriptors, day-of-month and day-of-week combined as the controller combines
them, on the wall clock of `spec.timezone` across daylight-saving changes. An
expression the controller would refuse shows `?`, and the info panel says why;
a date that never comes shows `never`.

`i` opens the info panel for the selected row, below the table or, on a pane at
least 140 columns wide, beside it: every schedule, the time zone, the next five
run times, the concurrency policy, the starting deadline, the history limits,
the suspend state, the last run, the active runs, the controller's conditions,
and the entrypoint and arguments of the workflow each run starts. Argument
values are shown, and `v` hides them for the selected row; under
`redactValues` they start hidden and `v` reveals that row only. `f` shows the
whole manifest the same way.

`enter` lists the workflows the cron workflow started: the workflow list,
narrowed on the server by the `workflows.argoproj.io/cron-workflow` label the
controller puts on each run. The pane title names the cron workflow, `esc`
returns to the cron list with the cursor where it was, and a workflow opened
from there returns to it with a second `esc`. The cron list refreshes on the
list's poll interval while it is shown. Suspending, resuming and submitting a
cron workflow are not available yet.

### Workflow templates

`:tmpl` lists the namespace's WorkflowTemplates and `:cwftmpl` the cluster's
ClusterWorkflowTemplates: name, entrypoint, how many templates and parameters
each defines, age, and on a wide pane the description the Argo UI shows. Cluster
templates belong to no namespace, so the header reads `ns: (cluster-scoped)`
there and `n` and `0` do nothing. The info panel (`i`) lists the entrypoint, the
arguments with their defaults, allowed values and descriptions (`v` hides or
reveals the values, as on the cron list), every template with its type (container, script, dag, steps,
suspend, resource, data, http, plugin or containerSet), the service account and
the labels. `enter` lists the workflows submitted from the template, by the
`workflows.argoproj.io/workflow-template` or `cluster-workflow-template` label,
and `esc` returns. Submitting a template is not available yet.

### Archived workflows

`:aw` (or `:archived`) lists the namespace's workflow archive: the runs the
controller copied to its database, which may already be gone from the cluster.
The columns are the workflow list's, newest first; `s` also sorts failures
first or by name. The list reads the newest 300 runs and says so when the
archive holds more. `enter` opens a run in the detail pane, read from the
archive by its UID; the title and the summary say it is archived, it is not
refreshed on the poll, and `a` explains that actions apply to live workflows
only. Node logs are asked for as usual, but an archived run's pods are usually
deleted with it: when nothing comes back, the log pane says so and names
`archiveLogs`, the workflow setting that keeps logs past the pods.

A server with no archive configured answers the list with an empty page, the
same answer as an empty archive, so the empty list says that too. Opening a run
on such a server, or listing on a server without the archive route, shows
"the workflow archive is not enabled on this server".

The profile picker (`P`, and the start screen) lists the profiles in your
config file with their server and namespace. Type to narrow the list; only a
configured profile can be chosen, because a name that is in no file names no
server to connect to.

The namespace picker offers configured `namespaces` and discovered namespaces;
you can also type a name. Discovery uses Argo's managed namespace or visible
workflows, so empty namespaces may need to be configured or typed.

Set `webURL` to the browser address of your Argo UI to use `o`. Clipboard
copy uses OSC52 and depends on terminal support. Log piping sends retained
lines to a command you enter, using `/bin/sh`; `pipeCommand` sets the initial
command (default `lnav`). Install that program separately.

## Skins

micko draws in the `default` skin unless told otherwise. It uses the
terminal's own 16-colour palette, so your terminal theme still applies. The
other skins set their own truecolor palettes:

`catppuccin-mocha`, `catppuccin-latte`, `gruvbox-dark`, `gruvbox-light`,
`nord`, `dracula`, `tokyo-night`, `solarized-dark`, `solarized-light`,
`one-dark`, `rose-pine`, `rose-pine-dawn`, `monokai`

`auto` asks the terminal for its background colour and picks
`catppuccin-mocha` on a dark one or `catppuccin-latte` on a light one. It
stays on `default` if the terminal does not answer.

Choose one with `skin:` at the top of the config file, or per profile to tell
clusters apart at a glance; `--skin NAME` overrides both, and also works with
`--demo`. An unknown name stops the program at startup and lists the valid
ones. `NO_COLOR` turns every skin into plain text. Colour never carries
meaning on its own: phases keep their glyph and word, and the header still
spells out `READ ONLY` or `ACTIONS ENABLED`.

## Flags

| Flag | Purpose |
| --- | --- |
| `--config PATH` | Select the configuration file |
| `--profile NAME` | Connect to this profile instead of opening the picker |
| `--server URL`, `--namespace NAME` | Override the profile endpoint or workflow namespace |
| `--token-file PATH`, `--ca-file PATH` | Override credential file or CA bundle |
| `--refresh-interval DURATION` | Poll interval, default `5s`; accepted range `1s`–`10m` |
| `--allow-actions` | Enable confirmed workflow actions (resume, suspend, retry, resubmit, stop, terminate, delete) |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification |
| `--debug` | Emit sanitized lifecycle diagnostics |
| `--skin NAME` | Colour skin, overriding the config file (see [Skins](#skins)) |
| `--redact-values` | Open every workflow with parameter and output values hidden |
| `--mascot` | Perch Mićko on the pane (terminals of 80×40 and larger) |
| `--demo` | Run the offline, read-only demo |
| `--version` | Print version and exit |

`--token-file` cannot be combined with a profile's `tokenEnv`; remove the
environment source from that profile before switching to a file.

## Troubleshooting and limits

- Startup errors name missing or invalid settings. Managed forwarding needs
  a complete target; every real connection needs an endpoint, namespace and
  configured token source.
- Authentication and permission failures are distinct. Check the token and
  Argo auth mode for 401; check access to the selected namespace for 403.
- A stale banner retains the last successful data while the connection recovers.
- Search and sorting apply to the collected workflow snapshot, capped at 5,000
  entries, in the all-namespaces view as well. Logs retain at most 10,000 lines or 8 MiB; pausing stops scrolling,
  not collection. Deleted pods or unavailable archived logs may prevent viewing logs.
- Parameter and output values are shown. Set `redactValues: true` at the top
  of the config file or on a profile, or pass `--redact-values`, to open every
  workflow with them hidden; `v` reveals them for the session. Log text,
  copied text and pipe output may contain sensitive application data either
  way.
- Workflow submission and parameter editing are not exposed in the UI.

Project history records live testing of v0.2.0 on macOS arm64 against Argo
Workflows v4.1.2 in server auth mode without TLS, including list/watch, node
logs, Resume and Stop. This does not establish live coverage of other server
versions, auth modes or all actions. Windows was cross-built, not validated
interactively; log piping currently requires `/bin/sh`.

See [development and testing](docs/development.md) for the code map, test
commands and opt-in cluster tests, and the [changelog](CHANGELOG.md) for
release history.

[Hands-on testing notes](docs/handson-testing.md) preserve operator feedback
and the changes made during live use.

## License

micko is free software under the GNU General Public License version 3; see
[LICENSE](LICENSE). Copyright (C) 2026 Filip Biljic.

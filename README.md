# micko

A keyboard-first terminal UI for [Argo Workflows](https://argoproj.github.io/workflows/).

![micko browsing the demo dataset: the workflow list, a failed run's timeline and explanation, the filter, the cron list, and Mićko the mascot](docs/demo.gif)

- **Browse** every run: its phase, its progress and why it failed, in one list.
- **Understand** a run: a timeline of how it ran and a plain-words explanation
  of how it ended.
- **Follow** it: logs labelled by the step that wrote them, and live
  Kubernetes events.
- **Act** on it: resume, suspend, retry, resubmit, stop, terminate or delete,
  each one confirmed, none retried.

## Try it

Requires Go 1.25 or later. The demo runs on synthetic data: no cluster, no
credentials, no writes.

```sh
git clone https://github.com/ficaa1/micko.git
cd micko
make build
./dist/micko --demo
```

Press `?` for every key and `q` to quit. Without Make, use
`go build -o dist/micko ./cmd/micko`; `make build` also stamps the Git commit
into the binary, and `--version` prints both.

## Install

The repository is private, so `go install` and Homebrew cannot fetch it yet:
both need anonymous access. Until it is public, download a binary from
[the releases page](https://github.com/ficaa1/micko/releases) while signed in,
verify it and put it on your PATH:

```sh
VERSION=0.7.1   # the release you want
gh release download "v$VERSION" --repo ficaa1/micko \
  --pattern '*_darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing   # sha256sum -c on Linux
tar -xzf "micko_${VERSION}_darwin_arm64.tar.gz"
```

## Connect

Copy [the example config](docs/micko.config.example.yaml) to
`~/.config/micko/config.yaml` and edit it for your cluster. One profile per
cluster:

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

Run `./dist/micko` to choose a profile from the picker, or
`./dist/micko --profile dev` to connect straight away. `P` switches profile
at any time; a switch is a full reconnection.

With `kubeContext` and `service` set, micko runs `kubectl port-forward` for
you on a free loopback port and closes it on exit; it needs `kubectl` on PATH
and access through that context. The profile's `server` then supplies only
the scheme and path prefix. For an endpoint you can already reach, leave out
`kubeContext`, `service`, `serviceNamespace` and `remotePort`, and `server` is
used as it is.

micko reads `$XDG_CONFIG_HOME/micko/config.yaml`, then
`~/.config/micko/config.yaml`, then the OS user config directory, and uses the
first that exists; `--config PATH` names one. With no config file the picker
shows the path to write and a sample profile.

### Authentication and TLS

Set exactly one of `tokenEnv` (an environment variable name) or `tokenFile`
(an absolute path); it is read for each request. For Argo's server auth mode,
keep `tokenEnv: MICKO_TOKEN` and leave the variable empty: an empty token
sends no Authorization header. For client auth, supply a bearer token Argo
accepts; the Kubernetes context does not provide one, and there is no
interactive SSO login.

HTTPS verifies certificates; `--ca-file` adds a CA, and
`--insecure-skip-tls-verify` turns verification off for one run with a
warning. Plain HTTP is allowed only to loopback addresses, and redirects are
rejected. Never put a literal token in a command line or a committed config.

## Tour

### The workflow list

Every run in the namespace, refreshed every five seconds. The phase is a
glyph, a word and a colour, so it reads without colour too. Suspended
workflows sort first and their waiting nodes are marked `AWAITING RESUME`.
`enter` opens a run; `T`, `X` and `E` open it straight on its timeline,
explanation or events, and `l` on its logs.

`w` adds wide columns: PROGRESS (the server's done/total pod count, with a
bar), STARTED and FINISHED in local time, TEMPLATE, CRON and LABELS. As the
pane narrows they drop in the order LABELS, CRON, FINISHED, MESSAGE,
TEMPLATE, STARTED, PROGRESS. A pane of 120 columns or more shows PROGRESS
without `w`.

### Filtering

`/` filters as you type, with a small query language. A plain word matches
names:

| Query | Matches |
| --- | --- |
| `etl report` | names containing `etl` and `report` (every term must match) |
| `etl\|report` | names containing either |
| `!etl` | names not containing `etl`; `!a\|b` is "not a, or b" |
| `/^demo-.*-\d+$/` | names matching a Go regular expression, case-insensitive |
| `~ddp` | names containing `d`, `d`, `p` in that order (fuzzy) |
| `phase=failed`, `phase!=succeeded` | the phase; `running` includes `suspended` |
| `age<2h`, `age>1d` | time since it started, or was created if it has not |
| `dur>10m` | run time; a running workflow counts until now |
| `label:team=data`, `label:team`, `label:!team` | a label's value, presence or absence |
| `tmpl=nightly` | started from this WorkflowTemplate or ClusterWorkflowTemplate |
| `cron=etl-hourly` | started by this CronWorkflow |

Durations combine `s`, `m`, `h` and `d`, as in `1d12h`. A term that does not
parse shows its error in the toolbar and keeps the previous filter; `esc`
cancels the edit. The toolbar shows the filter as it was read, such as
`phase=Failed & age<2h`. `p` cycles a phase filter. Filtering and sorting run
on the collected snapshot, not on the server.

### Nodes

The Nodes tab draws the workflow the way it runs, not the way the controller
records it. Steps sit under their Steps node in order. A DAG lists its tasks
in dependency order with what each waits for (`← extract`). Retry attempts
sit under their Retry node with their exit codes, loop items under their
task group, and the exit handler is a tree of its own.

Above the tree, a progress line gives `done/total` with a bar, the work by
state (`✓ 7  ● 3  ○ 1  ✗ 1`) and the elapsed time against the controller's
estimate (`elapsed 4m12s of ~9m`). As the terminal widens, rows gain the
template, the duration, a timing bar and the message.

`space` folds a subtree, `left` folds or climbs to the parent, `right`
unfolds; earlier retry attempts start folded. `i` opens everything the
workflow says about a node: IDs, template, phase and message, times, pod,
host, exit code, resource usage, inputs and outputs. `/` finds a node by name
and `n`/`N` step through the matches.

### Timeline

`T` draws the run as a Gantt chart. Pods and approval gates are bars coloured
by phase; DAG, Steps and Retry nodes bracket the time their children took.
The axis steps in seconds, minutes, hours or days as the run needs, and a
running workflow ends at a `now` line.

A shaded stretch (`░`) before a bar is time the node waited after what it
depended on had finished: a retry's backoff, or a queue for a free slot.
Nodes marked `◆` are the critical path, the chain of work that set the end
time; shortening anything else would not have finished the run sooner. Rows
share the Nodes tab's order and folds, `i` opens the same info panel and
`enter` opens the pod's log.

### Explain

`X` says why the workflow ended the way it did, as findings built from what
it records and nothing else: no network service, no model, the same answer
every time. Each finding is a card with a severity (`✗ ERROR`, `▲ WARNING`,
`◇ INFO`), a headline, the evidence and a next step.

The rules find the node that failed first, not the groups that failed because
of it; the attempts of an exhausted retry and whether they failed the same
way; out-of-memory kills; what exit codes 1, 2, 126, 127, 137, 139 and 143
mean; image pull errors and other reasons a pod never started; deadlines; a
spec the controller rejected; nodes that never ran; the exit handler; a gate
waiting for a person; a run past its estimate; and, for a run that succeeded,
steps that needed a retry.

For a failed pod it reads the last 200 lines of the `main` container's log
and quotes up to eight: error lines, whole tracebacks and the line before
each. `y` copies the report as plain text for an incident channel, and `l`
opens the failing pod's full log.

### Logs

A workflow's logs mix every pod, so each line starts with the step that wrote
it, coloured per step; `L` hides the labels. `t` follows the tail, `space`
pauses scrolling (collection carries on), `c` picks the container and `w`
wraps long lines. Lines whose level is `ERROR`, `WARN` or similar, in text or
in a JSON `level` or `severity` field, get that word coloured.

`/` searches, `n`/`N` step through matches, and `&` shows only matching lines,
as in `less`. `ctrl+t` asks the server to prefix each line with its own
timestamp, reopening the stream. `|` pipes the retained lines to a command
through `/bin/sh`; `pipeCommand` sets the default (`lnav`). Logs keep at most
10,000 lines or 8 MiB.

### Events

`E` streams the Kubernetes events about the workflow and its pods while the
section is open: `WorkflowRunning`, `WorkflowNodeFailed`, `Scheduled`,
`Pulled`, `BackOff`, `FailedScheduling` and the like, with time, type, reason,
object, count and message. `s` puts warnings first, `/` filters and `y`
copies the table. A dropped stream reconnects with back-off. Kubernetes keeps
events for about an hour by default, and the token needs permission to watch
events in the namespace.

### Command palette

`:` opens the palette on every screen. Commands rank as you type, `tab`
completes and `enter` runs; a word that names no command is reported, never
guessed. After `ns` and `profile` a space completes the name, and
`ctrl+p`/`ctrl+n` step through earlier commands.

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
| `mascot` | Move Mićko: perch, floor, off |
| `help` | Show every key |
| `quit`, `q` | Quit |

### Cron workflows, templates and the archive

`:cron` lists CronWorkflows with their schedules, time zone, suspend state,
active runs, last run and NEXT RUN, which micko computes the way the
controller does, time zones and daylight saving included. `i` opens an info
panel with the next five runs, the policies and history limits, the
controller's conditions and the arguments each run gets.

`:tmpl` and `:cwftmpl` list WorkflowTemplates and ClusterWorkflowTemplates:
entrypoint, template and parameter counts, age and description; `i` lists
every argument and template. On all three, `enter` lists the workflows that
row started, `v` hides or reveals values and `f` shows the manifest.

`:aw` lists the workflow archive, newest first, including runs already gone
from the cluster. `enter` opens one in the detail view; actions apply to live
workflows only, and an archived run's logs are usually gone unless the
workflow set `archiveLogs`.

### Actions

Actions are on and every one asks first. `--read-only` turns them off for a
session and the header says `READ ONLY`. Press `a` on the list or in a run:

| Key | Verb | Applies to |
| --- | --- | --- |
| `u` | resume | a suspended workflow |
| `z` | suspend | a running workflow that is not suspended |
| `r` | retry | a failed or errored workflow |
| `b` | resubmit | a finished workflow |
| `s` | stop | a running workflow; graceful, runs exit handlers |
| `t` | terminate | a running workflow; no exit handlers |
| `d` | delete | any workflow |

Only `y` confirms; `enter` and `esc` cancel. Terminate asks you to type the
workflow's name. Delete asks twice: after `y`, only `D` (shift+d) deletes.

Before sending, micko reads the workflow again and refuses if its identity
changed or the verb no longer applies. It sends one request and never retries
a write. The outcome is `CONFIRMED` when the end state was observed,
`ACCEPTED` when the server applied it but the end state was not seen within
five seconds, `REFUSED` when nothing was sent, and `UNKNOWN` when it may or may
not have been applied, so inspect the workflow before acting again.

`space` marks rows (`◆`); marks survive refreshes, sorting and filtering.
With marks, `a` acts on all of them: the menu counts how many each verb
applies to, and the requests go one at a time, each checked on its own.
`esc` stops the run after the request in flight. The result lists every
outcome until you close it.

Every write attempt, refused ones included, adds one JSON line to
`~/.local/state/micko/actions.jsonl` (under `$XDG_STATE_HOME` when set):
time, profile, server, namespace, name, UID, verb, outcome and error. Set
`journal: false` to turn it off. A journal that cannot be written never
blocks an action.

### Namespaces and profiles

`n` opens the namespace picker: configured `namespaces`, the ones micko
discovers, or a name you type. `0` or `:all` lists every namespace the token
may read, with a NAMESPACE column, and `/` then matches `namespace/name`.
`P` opens the profile picker, which is also the start screen without
`--profile`.

`o` opens the workflow in the Argo UI when the profile sets `webURL`. `y`
copies through OSC52, so it depends on your terminal. `f` shows the current
view full screen without borders.

### Skins

The `default` skin uses your terminal's own 16 colours. Thirteen truecolor
skins set their own: `catppuccin-mocha`, `catppuccin-latte`, `gruvbox-dark`,
`gruvbox-light`, `nord`, `dracula`, `tokyo-night`, `solarized-dark`,
`solarized-light`, `one-dark`, `rose-pine`, `rose-pine-dawn` and `monokai`.
`auto` picks `catppuccin-mocha` or `catppuccin-latte` from your terminal's
background.

Set `skin:` at the top of the config or per profile, to tell clusters apart
at a glance; `--skin NAME` overrides both and works with `--demo`.
`NO_COLOR` turns every skin into plain text, and colour never carries meaning
on its own.

### Mićko

micko is named after Mićko, my eastern rosella, and he is its mascot. He is
off by default. `--mascot`, `mascot: true` or `:mascot` perches him on top of
the pane, where he blinks, dozes and looks around. `--mascot=floor`,
`mascot: floor` or a second `:mascot` sits him in the pane's bottom corner,
where now and then he hops over to kiss himself in his mirror. He needs a
terminal of 80×40 or larger and costs the pane three rows perched, four on
the floor.

## Keys

| View | Keys |
| --- | --- |
| Navigation | `j`/`k` or arrows; `pgup`/`pgdn`; `gg`/`G` or `home`/`end` |
| Workflow list | `enter` open, `T` timeline, `X` explain, `E` events, `l` logs, `/` filter, `s` sort, `p` phase, `r` refresh, `n` namespace, `0` all namespaces, `space` mark, `a` actions, `w` wide columns, `esc` clear marks then filter |
| Cron workflows, templates, archive | `enter` its workflows (a run, on the archive), `i` info panel, `v` hide or reveal values, `/` search, `s` sort, `n` namespace, `0` all namespaces, `f` manifest |
| Detail | `tab`/`shift+tab` switch Summary, Nodes, Timeline, Explain, Events and Resource; `1`–`9` jump to a section; `T`, `X`, `E` jump; `r` refresh; `a` actions; `v` reveal values |
| Nodes and Timeline | `enter`/`l` logs, `space` fold, `left`/`right` fold or climb/unfold, `i` node info; on Nodes also `/` find, `n`/`N` matches, `h` skipped nodes, `s` sort, `p` phase |
| Explain | `y` copy the report, `l` the failing pod's full log |
| Events | `s` warnings first, `/` filter, `y` copy, `r` restart the stream |
| Logs | `t` follow, `G` newest, `space` pause, `c` container, `/` search, `n`/`N` matches, `&` only matching, `w` wrap, `L` labels, `ctrl+t` server timestamps, `\|` pipe |
| Actions | `u` resume, `z` suspend, `r` retry, `b` resubmit, `s` stop, `t` terminate, `d` delete; `y` confirms, `D` finishes a delete |
| Anywhere | `:` palette, `P` profile, `f` full screen, `y` copy, `o` open in Argo UI, `?` help, `esc` back, `q` quit outside text entry, `ctrl+c` quit |

## Flags

| Flag | Purpose |
| --- | --- |
| `--config PATH` | Select the configuration file |
| `--profile NAME` | Connect to this profile instead of opening the picker |
| `--server URL`, `--namespace NAME` | Override the profile endpoint or workflow namespace |
| `--token-file PATH`, `--ca-file PATH` | Override the credential file or CA bundle |
| `--refresh-interval DURATION` | Poll interval, default `5s`, from `1s` to `10m` |
| `--read-only` | Turn off workflow actions |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification |
| `--redact-values` | Open every workflow with parameter and output values hidden |
| `--skin NAME` | Colour skin, overriding the config file |
| `--mascot`, `--mascot=floor` | Put Mićko on the pane, perched or on the floor |
| `--debug` | Emit sanitized lifecycle diagnostics |
| `--demo` | Run the offline, read-only demo |
| `--version` | Print version and commit, then exit |

`--token-file` cannot be combined with a profile's `tokenEnv`.

## Limits and troubleshooting

- Startup errors name the missing or invalid setting, never a secret.
- A 401 means the token or Argo's auth mode; a 403 means access to the
  namespace. The server's permissions always decide what an action may do.
- When the connection drops, a stale banner keeps the last data on screen
  while micko recovers.
- The list collects at most 5,000 workflows, in the all-namespaces view too.
- Parameter and output values are shown. Set `redactValues: true` in the
  config or on a profile, or pass `--redact-values`, to hide them until `v`.
  Logs, copied text and pipe output can still contain sensitive data.
- micko cannot submit workflows, edit parameters, or suspend, resume or
  submit cron workflows and templates.
- Argo's action endpoints address workflows by name, so a same-name
  replacement between micko's identity check and the write is possible.
- Tested live against Argo Workflows v4.1.2 on macOS arm64, in server auth
  mode without TLS. Windows builds but is untested, and log piping needs
  `/bin/sh`.

## Development

See [development and testing](docs/development.md) for the architecture,
local checks and the opt-in cluster tests, and the [changelog](CHANGELOG.md)
for release notes.

## License

micko is free software under the GNU General Public License version 3; see
[LICENSE](LICENSE). Copyright (C) 2026 Filip Biljic.

# argo-tui

A keyboard-first terminal UI for [Argo Workflows](https://argoproj.github.io/workflows/).
Browse workflows, inspect nodes and resources, stream logs, and resume, retry,
resubmit or stop a run with explicit confirmation.

![argo-tui browsing the demo dataset](docs/demo.gif)

## Build and try it

Requires Go 1.25 or later; the module selects Go 1.25.4 as its toolchain.

```sh
git clone https://github.com/ficaa1/argo-tui.git
cd argo-tui
make build
./dist/argo-tui --demo
```

Without Make, use `go build -o dist/argo-tui ./cmd/argo-tui`. `make build`
also stamps the Git commit into the binary. `--version` prints both version
and commit. The demo uses synthetic data without connecting to a cluster or
reading credentials. Press `?` for help and `q` to quit.

## Install

The repository is private, so `go install` and Homebrew cannot fetch it: both
need anonymous access. The release job still builds the binaries, the
checksums and the tap formula, so both paths start working on the day the
repository becomes public.

Until then, download a binary from
[the releases page](https://github.com/ficaa1/argo-tui/releases) while signed
in, verify it and put it on your PATH:

```sh
gh release download v0.3.1 --repo ficaa1/argo-tui \
  --pattern '*_darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing   # sha256sum -c on Linux
tar -xzf argo-tui_0.3.1_darwin_arm64.tar.gz
```

## Connect

Copy [the example config](docs/argo-tui.config.example.yaml) to
`~/.config/argo-tui/config.yaml` and edit it for your cluster. A minimal
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
    tokenEnv: ARGO_TUI_TOKEN
```

Start it with no arguments and it opens the profile picker:

```sh
./dist/argo-tui
```

Or name the profile to connect to it directly:

```sh
./dist/argo-tui --profile dev
```

Managed forwarding requires `kubectl` on PATH and access through the named
Kubernetes context. argo-tui starts its own forward on a free loopback port
and closes it on exit. The profile's `server` supplies the scheme and path
prefix; the forward supplies the host and port. Use HTTPS if the Argo Server
expects TLS. For an already reachable endpoint, omit `kubeContext`, `service`,
`serviceNamespace` and `remotePort`; `server` is then used directly.

Configuration lookup checks `$XDG_CONFIG_HOME/argo-tui/config.yaml`, then
`~/.config/argo-tui/config.yaml`, then the OS user config directory, using
the first existing file. `--config PATH` selects a file explicitly.
With no `--profile` and no `--server`, argo-tui opens the profile picker and
connects to nothing until you choose. `currentProfile` places the cursor on a
row; it does not connect by itself. `P` reopens the picker at any time, and
switching profile is a full reconnection: in-flight requests are canceled, the
snapshot is dropped and the port-forward is closed before the next one starts.
With no config file the picker shows the path to write and a sample profile.

### Authentication and TLS

Configure exactly one of `tokenEnv` (an environment variable name) or
`tokenFile` (an absolute file path). The source is read for each request.
The current config validator requires a source even when no token is needed:
for Argo's server auth mode, keep `tokenEnv: ARGO_TUI_TOKEN` and leave that
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
./dist/argo-tui --profile dev --allow-actions
```

Open a workflow and press `a`, then `u` (resume), `r` (retry), `b` (resubmit)
or `s` (stop). The confirmation identifies the target; only `y` confirms.
Enter and Esc cancel. Stop is graceful and runs exit handlers.

The app checks identity before sending a mutation and never automatically
retries a write. An `UNKNOWN` outcome means the server may have applied it;
inspect the workflow before deciding what to do next. The server's permissions
remain authoritative. Argo's action endpoints address workflows by name and
have no UID precondition, so a same-name replacement between the identity
check and the write remains possible.

## Everyday keys

| View | Keys |
| --- | --- |
| Navigation | `j`/`k` or arrows; `pgup`/`pgdn`; `gg`/`G` or `home`/`end` |
| Workflow list | `enter` open, `T` open on its timeline, `X` open on its explanation, `l` workflow logs, `/` search, `s` sort, `p` phase, `n` namespace |
| Detail | `tab`/`shift+tab` switch Summary, Nodes, Timeline, Explain and Resource; `1`–`9` jump to a section by position; `T` timeline; `X` explain; `r` refresh; `a` actions |
| Nodes | `enter`/`l` selected node's logs, `space` fold/unfold, `left`/`right` fold or climb/unfold, `i` node info, `/` find by name, `n`/`N` next/previous match, `h` show skipped nodes, `s` sort, `p` phase filter |
| Timeline | `enter`/`l` selected node's logs, `space` fold/unfold, `left`/`right` fold or climb/unfold, `i` node info |
| Explain | `y` copy the report, `l` the failing pod's full log, `v` hides or reveals parameter values |
| Resource | `v` hides or reveals parameter/output values (also in the node info panel) |
| Logs | `t` follow tail, `space` pause scrolling, `c` container, `/` search, `n`/`N` matches, `\|` pipe |
| Display | `f` borderless full screen, `y` copy, `o` open workflow in Argo UI |
| General | `P` switch profile, `?` help, `esc` back/cancel, `q` quit outside text entry, `ctrl+c` quit globally |

Suspended workflows sort first by default and their waiting nodes are marked
`AWAITING RESUME`. Node logs require a known pod name: the adapter uses the
workflow's pod naming annotation and leaves the shortcut unavailable when
it cannot resolve one safely.

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

argo-tui draws in the `default` skin unless told otherwise. It uses the
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
| `--allow-actions` | Enable confirmed Resume, Retry, Resubmit and Stop |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification |
| `--debug` | Emit sanitized lifecycle diagnostics |
| `--skin NAME` | Colour skin, overriding the config file (see [Skins](#skins)) |
| `--redact-values` | Open every workflow with parameter and output values hidden |
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
  entries. Logs retain at most 10,000 lines or 8 MiB; pausing stops scrolling,
  not collection. Deleted pods or unavailable archived logs may prevent viewing logs.
- Parameter and output values are shown. Set `redactValues: true` at the top
  of the config file or on a profile, or pass `--redact-values`, to open every
  workflow with them hidden; `v` reveals them for the session. Log text,
  copied text and pipe output may contain sensitive application data either
  way.
- Terminate exists in the transport but has no UI shortcut. Workflow submission,
  bulk actions and parameter editing are not exposed in the UI.

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

argo-tui is free software under the GNU General Public License version 3; see
[LICENSE](LICENSE). Copyright (C) 2026 Filip Biljic.

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
| Workflow list | `enter` open, `l` workflow logs, `/` search, `s` sort, `p` phase, `n` namespace |
| Detail | `tab`/`shift+tab` switch Summary, Nodes and Resource; `r` refresh; `a` actions |
| Nodes | `l` selected node's logs, `h` show skipped nodes, `p` phase filter |
| Resource | `v` hides or reveals parameter/output values |
| Logs | `t` follow tail, `space` pause scrolling, `c` container, `/` search, `n`/`N` matches, `\|` pipe |
| Display | `f` borderless full screen, `y` copy, `o` open workflow in Argo UI |
| General | `P` switch profile, `?` help, `esc` back/cancel, `q` quit outside text entry, `ctrl+c` quit globally |

Suspended workflows sort first by default and their waiting nodes are marked
`AWAITING RESUME`. Node logs require a known pod name: the adapter uses the
workflow's pod naming annotation and leaves the shortcut unavailable when
it cannot resolve one safely.

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

# argo-tui

A keyboard-first terminal UI for [Argo Workflows](https://argoproj.github.io/workflows/).

It lists workflows, opens one, walks its node tree, streams the logs of a
single node, and — only when you ask for it — resumes or stops a run. It
reaches the Argo Server through a `kubectl port-forward` that it starts and
stops itself, so nothing has to be exposed.

Version `0.2.0`. Verified against a real Argo Workflows v4.1.2 server.

```
argo-tui 0.2.0 @ a1b2c3d   server: http://127.0.0.1:2746   ns: batch-cd-prd      READ ONLY
┌─ Workflows ──────────────────────────────────── list: 100 workflows | mode: watch ─┐
│ Search: (none)  / to filter by name  Phase: All  5 awaiting resume                 │
│ NAME                              PHASE           AGE  DURATION                    │
│ deploy-multi-layer-p4r8w          ◐ Suspended     39m  ongoing                     │
│ deploy-all-regions-r5t9z          ◐ Suspended      1d  ongoing                     │
│ pr-diff-vertex-endpoints-skl62    ✗ Failed      1h55m  4m                          │
│ web-users-full-deploy-6gq4k       ✓ Succeeded   1h17m  4m                          │
└────────────────────────────────────────────────────────────────────────────────────┘
list   enter open  l logs  / search  s sort  p phase  n namespace  r refresh    ? help   1-39/100
```

## Build

You need Go 1.25 or later. There is nothing else to install.

```sh
git clone <your-fork> argo-tui
cd argo-tui
make build            # or: go build -o dist/argo-tui ./cmd/argo-tui
./dist/argo-tui --version
```

`make build` stamps the commit into the binary. `go build` alone does not, and
the header then shows `unknown` where the commit goes.

Cross-compile for another machine:

```sh
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -o dist/argo-tui-linux  ./cmd/argo-tui
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/argo-tui-macos  ./cmd/argo-tui
```

## Quick start

### 1. Look at it with no cluster

The demo uses built-in data. It opens no socket and writes nothing.

```sh
./dist/argo-tui --demo
```

Press `?` for the keys and `q` to quit.

### 2. Point it at your own Argo

Write `~/.config/argo-tui/config.yaml`. A full commented example is in
[`docs/argo-tui.config.example.yaml`](docs/argo-tui.config.example.yaml).

```yaml
currentProfile: team-prod
refreshInterval: 5s

profiles:
  team-prod:
    # argo-tui runs `kubectl port-forward` against this context itself.
    kubeContext: my-project__my-cluster
    service: argo-workflows-server
    serviceNamespace: argo
    remotePort: 2746
    # Only the scheme and any path prefix are read from this URL. The host
    # and port always come from the forward argo-tui owns.
    server: http://127.0.0.1:2746
    # The namespace the session starts in. `n` switches it while it runs.
    namespace: reports-prd
    # Optional. Namespaces the picker always offers, on top of the ones the
    # server reports. Use it for a namespace that is often empty.
    namespaces:
      - reports-prd
      - workflows-tst
    # Optional. The browser address of this cluster's Argo UI, used by `o`.
    webURL: https://workflows.prd.example.com
    # Optional. What the log pipe editor (`|`) prefills with. Default: lnav.
    pipeCommand: lnav
```

Then start it:

```sh
./dist/argo-tui --profile team-prod
```

You need `kubectl` on your PATH and a working context. argo-tui starts the
forward on a free local port, and stops it when you quit.

**Tokens.** An Argo Server started with `--auth-mode=server` needs no client
token: leave the setting out and no `Authorization` header is sent. Otherwise
name an environment variable with `tokenEnv`, or a file with `tokenFile`.
Never put a token on the command line. The token is read per request and is
never written to the config.

### 3. Turn on actions

Resume, Retry, Resubmit and Stop are off unless you ask for them:

```sh
./dist/argo-tui --profile team-prod --allow-actions
```

Even then, every action opens a pane that names the exact target, its UID and
the server, and waits for a `y`. argo-tui sends the mutation once. If the
result is ambiguous it says so and stops; it never retries a write.

The header states which mode you are in: `READ ONLY` or `ACTIONS ENABLED`.

## Keys

Press `?` in the program for the full list. The ones you will use first:

| Key | What it does |
| --- | --- |
| `j` `k`, arrows | move |
| `pgup` `pgdn`, `ctrl+u` `ctrl+d` | page |
| `gg` `G`, `home` `end` | first / last row |
| `enter` | open the selected workflow |
| `tab` | next section: Summary, Nodes, Resource |
| `l` | logs — for the selected **node** in the Nodes tab |
| `h` | show or hide skipped nodes |
| `/` | filter the list; it narrows as you type |
| `s` `p` | change the sort, change the phase filter |
| `p` | in the Nodes tab, narrow to one node phase |
| `n` | switch namespace (list); next search match (logs) |
| `N` | previous search match (logs) |
| `\|` | pipe the retained log lines to another program |
| `r` | refresh now |
| `f` | full-screen view with no borders, so a mouse selection stays clean |
| `y` | copy to the clipboard |
| `o` | open the workflow in the Argo UI, and copy the link |
| `t` | follow the log tail again (logs only) |
| `a` | actions, with `--allow-actions` |
| `esc` | back |
| `q` | quit |

## What it is for

The job it was built around: a deployment stops at a manual approval gate,
and you have to read the plan before you approve it.

1. Suspended runs sort to the top of the list and read `◐ Suspended`. The
   toolbar counts them.
2. Open one and press `tab`. Skipped branches are hidden, so the tree shows
   what actually ran. The gate row is marked `AWAITING RESUME`.
3. Move to the node you care about and press `l`. Argo does not publish pod
   names, so argo-tui derives each one with Argo's own algorithm. When the
   server records no naming scheme, the key stays inert rather than guessing.
4. Press `f` to read the logs full screen, or `y` to copy them.
5. Press `esc`, then `a`, then resume.

## Flags

| Flag | Meaning |
| --- | --- |
| `--profile NAME` | the profile to use from the config |
| `--config PATH` | a config file elsewhere |
| `--demo` | built-in data, no network |
| `--allow-actions` | arm Resume and Stop |
| `--namespace`, `--server` | override the profile |
| `--refresh-interval` | poll interval, default `5s` |
| `--token-file`, `--ca-file` | token file, custom CA bundle |
| `--insecure-skip-tls-verify` | disable TLS checks (unsafe) |
| `--debug` | sanitized lifecycle diagnostics |
| `--version` | print the version and exit |

## Develop

```sh
make test        # focused package set
go test ./...    # everything
make vet
make lint-fmt
```

Golden files are refreshed with `UPDATE_GOLDEN=1 go test ./...`. Check the
diff before you commit one.

## Documentation

- [`docs/usage.md`](docs/usage.md) — the longer command reference
- [`docs/handson-testing.md`](docs/handson-testing.md) — testing notes against a real cluster, and what each one changed
- [`docs/security.md`](docs/security.md) — token handling, sanitization, action safety
- [`docs/protocol.md`](docs/protocol.md) — the Argo REST surface that is used
- [`docs/contracts.md`](docs/contracts.md) — the internal data contract

## Limits

- Kubernetes access is through `kubectl`. There is no in-process client and no
  SSO browser login.
- Starting with no profile uses `currentProfile`. There is no profile picker
  yet.
- Resume, Retry, Resubmit and Stop are the actions wired to the UI. Terminate
  exists in the transport but has no key.
- Argo has no endpoint that lists namespaces. The picker derives them from the
  workflows your token can read, so a namespace with no workflows does not
  appear in the list. Type its name and press enter, or put it in
  `namespaces:`.

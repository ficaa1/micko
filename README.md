# micko

```text
    .---.                _        _
   ( o o )    _ __ ___  (_)  ___ | | __  ___
  ((  V  ))  | '_ ` _ \ | | / __|| |/ / / _ \
   (     )   | | | | | || || (__ |   < | (_) |
  ~~"~~~"~~  |_| |_| |_||_| \___||_|\_\ \___/
```

A keyboard-first terminal UI for [Argo Workflows](https://argoproj.github.io/workflows/).

![micko: the workflow list, a failed run's timeline and explanation, the filter and the cron list](docs/demo.gif)

## Features

- **Workflow list** with phase, progress and failure reason, filtered by a small query language (`phase=failed age<2h`)
- **Timeline** of each run as a Gantt chart, with the critical path marked
- **Explain**: why a run failed, in plain words, from what the workflow records
- **Node tree** that reads like the pipeline: retries, DAG dependencies, exit handlers
- **Logs** labelled by step, with search, filtering and piping; **live Kubernetes events**
- **Actions**: resume, suspend, retry, resubmit, stop, terminate, delete, one at a time or in bulk, each confirmed
- **Cron workflows, templates and the archive**, with next-run times computed locally
- **Profiles** per cluster, with managed `kubectl port-forward`
- **13 colour skins**, and Mićko, an optional ASCII rosella

## Install

```sh
brew install ficaa1/tap/micko
```

With [mise](https://mise.jdx.dev):

```sh
mise use -g github:ficaa1/micko
```

Or `go install github.com/ficaa1/micko/cmd/micko@latest`, or download a binary
from [the releases page](https://github.com/ficaa1/micko/releases).

With [Nix](https://nixos.org/download/) and flakes enabled:

```sh
nix run github:ficaa1/micko -- --demo
nix profile install github:ficaa1/micko
```

The flake builds from source with pinned dependencies for Linux on x86_64
and ARM64, and macOS on ARM64. Its pinned Nixpkgs no longer supports Intel
macOS. It includes `kubectl` for managed port-forwarding and
`xdg-open` on Linux; commands already on your PATH take precedence.

From a checkout, run `nix build` to build `result/bin/micko`, or
`nix flake check` to build and run the default Go tests and version check.
When changing Go dependencies, update `vendorHash` in `nix/package.nix`;
set it to `lib.fakeHash`, build, then use the hash Nix reports.
The package version is read from `internal/buildinfo/buildinfo.go`.

## Quick start

micko reads profiles, one per cluster, from `~/.config/micko/config.yaml`.
Run `micko`, pick a profile, and press `?` for help.

**Through `kubectl port-forward`**, when the Argo Server is not exposed.
micko starts the forward itself on a free local port, using your kubeconfig,
and closes it on exit:

```yaml
profiles:
  dev:
    kubeContext: my-cluster          # the kubeconfig context to use
    service: argo-workflows-server   # the Argo Server's Service...
    serviceNamespace: argo           # ...in this namespace
    remotePort: 2746                 # ...on this port
    server: https://127.0.0.1:2746   # https or http, matching the Argo Server;
                                     # micko swaps in the forwarded host and port
    namespace: workflows             # where your workflows run
    tokenEnv: MICKO_TOKEN            # the environment variable holding your Argo token
```

**Straight to a reachable Argo Server**, with a bearer token:

```yaml
profiles:
  prod:
    server: https://argo.example.com # the Argo Server's address
    namespace: workflows
    tokenEnv: ARGO_TOKEN             # or tokenFile: /absolute/path/to/token
    # caFile: /absolute/path/to/ca.pem   for a private CA
```

```sh
export ARGO_TOKEN=$(argo auth token | sed 's/^Bearer //')  # micko adds "Bearer "
micko --profile prod
```

`tokenEnv` names an environment variable; `tokenFile` names a file. micko
reads the token from it on every request and never logs or shows it. If your
Argo Server runs with `--auth-mode=server` it needs no token: keep the
`tokenEnv` line and leave the variable unset. Plain `http` is only allowed to
loopback addresses.

No cluster at hand? `micko --demo` runs on synthetic data, with no
credentials and no writes.

## Compatibility

Tested live against Argo Workflows v4.1.2 on macOS arm64, through
`kubectl port-forward` to an Argo Server in server auth mode, so with no
token. A direct connection with a bearer token and other Argo versions are
untested; an issue saying how micko fared on yours is welcome.

Releases include macOS, Linux and Windows binaries. The Windows build has
not been tested yet; an issue saying how it ran for you is welcome.

## Documentation

- [Usage guide](docs/usage.md): connecting, authentication, every feature, flags and limits
- [Keybindings](docs/keybindings.md): every key and palette command
- [Example config](docs/micko.config.example.yaml): every config key

## Building from source

Requires Go 1.25 or later.

```sh
git clone https://github.com/ficaa1/micko.git
cd micko
make build        # or: go build -o dist/micko ./cmd/micko
./dist/micko --demo
```

## Security

See [SECURITY.md](SECURITY.md) to report a vulnerability and for how micko
handles your token.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md)
first, and [docs/development.md](docs/development.md) for the architecture
and the checks to run before pushing. Release notes are in the
[changelog](CHANGELOG.md).

## License

[GPL-3.0](LICENSE). Copyright (C) 2026 Filip Biljic.

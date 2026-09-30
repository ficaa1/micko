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

Or `go install github.com/ficaa1/micko/cmd/micko@latest`, or download a binary
from [the releases page](https://github.com/ficaa1/micko/releases).

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
    server: http://127.0.0.1:2746    # only the scheme and path are used; the forward picks the port
    namespace: workflows             # where your workflows run
    tokenEnv: MICKO_TOKEN            # leave the variable empty in Argo's server auth mode
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

Either way the token is read on every request and never logged or shown.
Plain `http` is only allowed to loopback addresses.

No cluster at hand? `micko --demo` runs on synthetic data, with no
credentials and no writes.

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

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md)
first, and [docs/development.md](docs/development.md) for the architecture
and the checks to run before pushing. Release notes are in the
[changelog](CHANGELOG.md).

## License

[GPL-3.0](LICENSE). Copyright (C) 2026 Filip Biljic.

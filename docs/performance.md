# Measuring performance

Measure before a performance change and after it, with the same session on
the same cluster. This page covers the tools for that: the `--debug` request
timings and spans, the scripted sessions in `scripts/perf`, and the
in-package benchmarks.

## Request timings

`--debug` writes one JSON line to stderr for every HTTP request, next to the
port-forward lifecycle lines. Redirect stderr to a file, or the lines draw
over the TUI:

```sh
micko --profile test --read-only --debug 2> diag.jsonl
```

```json
{"stage":"request","state":"ok","status":200,"endpoint":"get","conn":"reused","ttfb_ms":196,"total_ms":268,"bytes":465943}
```

| Field | Meaning |
| --- | --- |
| `endpoint` | The call, from a fixed set: `list`, `gate`, `get`, `info`, `namespaces`, `logs`, `watch`, `events`, `cron`, `templates`, `clustertemplates`, `archived`, `archivedget`, `action` |
| `state` | `ok`, or `failed` for a request that got no response. A canceled stream also reports `failed` |
| `conn` | `new` or `reused`. Through a port-forward, a new connection also opens a tunnel stream |
| `connect_ms`, `tls_ms` | Only on a new connection |
| `ttfb_ms` | Time to the response headers: mostly server time |
| `total_ms` | Time to the end of the body. For a stream (`logs`, `watch`, `events`) it is the stream's lifetime |
| `bytes` | Body bytes read |

The endpoint names come from `internal/argo/timing.go`; no path, namespace or
token reaches the file.

## Spans

A request timing shows the server and the network. A span shows what the
reader waited for: the time from the key or the connection to the reply that
changed the screen. `--debug` writes one line per span:

```json
{"stage":"span","state":"ok","span":"detail_open","total_ms":295}
```

| Span | Starts | Ends |
| --- | --- | --- |
| `first_list` | The connection is in use, at start or after a profile switch | The first list reply |
| `allns` | `0` turns on all namespaces | The all-namespaces list reply |
| `detail_open` | A workflow opens | Its detail reply |
| `first_log` | The logs pane opens | The first log lines, or the end of the stream |
| `cron_open`, `templates_open`, `clustertemplates_open`, `archived_open` | The route opens with no rows | Its list reply |

`state` is `failed` when the reply was an error. A span ends only on the reply
to the request that started it: a span whose request was replaced or
canceled writes no line. A span does not include the frame that follows the
reply; `TestEventsFrameCostDoesNotGrowWithEvents` shows how to measure that
frame.

A span longer than its request is time in micko: decoding, the update loop,
or keys and other replies in front of the reply.

## Scripted sessions

The scripts in `scripts/perf` run micko in tmux, send the same keys every
time, and read the timings back. Each one builds the working tree first, so a
run measures the code as it is now. They need `fish`, `tmux`, `jq`, `perl`
and `kubectl` with access to the profile's cluster.

Every session starts micko with `--read-only`: no workflow action can be
sent, and the sessions only read. They still load the cluster's Argo server
and open a port-forward to it.

1. Pick a large workflow, so the detail view's cost is visible:

   ```sh
   fish scripts/perf/largest.fish <kube-context> <namespace>
   ```

2. Run the session once for each version you compare. Check out the version,
   then run with a label that names it:

   ```sh
   fish scripts/perf/session.fish before prod long-workflow-12345
   # change the code
   fish scripts/perf/session.fish after prod long-workflow-12345
   ```

   The session waits for the first list, stays on the list for 30 s, opens
   the workflow, stays in the detail view for 60 s and quits. Two optional
   arguments change the 30 and the 60.

3. Compare:

   ```sh
   fish scripts/perf/report.fish before after
   ```

   ```text
   == after
     forward ready 1480 ms, first list 2079 ms
     span first_list: 820 ms
     span detail_open: 295 ms
     list (32 s): 3 requests, 69 KiB
       gate: 1 x, 0 KiB, median 34 ms, failed 0
       info: 1 x, 0 KiB, median 108 ms, failed 0
       list: 1 x, 69 KiB, median 578 ms, failed 0
     detail (61 s): 1 requests, 455 KiB
       get: 1 x, 455 KiB, median 219 ms, failed 0
   ```

`allns.fish <label> <profile>` measures one key instead: the time from `0`
(all namespaces) to the list answer, and the `allns` span.

Output goes to `perf-out/`, which Git ignores: the binary, the diagnostics,
the phase marks and a capture of the detail screen. If the detail capture
does not show the workflow, the search did not find it and the detail phase
measured the list.

Request and byte counts repeat from run to run; use them to judge a change.
Latencies move with the cluster and the port-forward, so compare them over
several runs.

## Benchmarks and allocation tests

Drawing cost is measured in-package, without a cluster:

```sh
go test -bench . -count 10 ./internal/ui/... > before.txt
# change the code
go test -bench . -count 10 ./internal/ui/... > after.txt
go run golang.org/x/perf/cmd/benchstat@latest before.txt after.txt
```

A regression test for drawing cost counts allocations with
`testing.AllocsPerRun` rather than timing it; the count is exact on every
machine. `TestEventsFrameCostDoesNotGrowWithEvents` is the pattern.

## Reference results

Team-prod, namespace `workflows`, workflow `long-workflow-12345`
(335 nodes, 455 KiB per detail read), measured 2026-10-01.

| Commit | List, 30 s | Detail, 60 s |
| --- | --- | --- |
| `a1bff8b` | 17 requests, 412 KiB, 5 watches restarted | 13 `get`, 5,915 KiB |
| `5b2bed6` | 3 requests, 69 KiB | 1 `get`, 455 KiB |

`allns.fish` on `5b2bed6`: the first all-namespaces list starts 11–14 ms after
the key, against 97–133 ms on `2aa28ec`.

Spans, measured when they were added, same workflow: `first_list` 820 ms
(list request 778 ms), `detail_open` 295 ms (`get` 274 ms), `allns` 514–518 ms
(list request 480 ms, body read 488–494 ms after the key).

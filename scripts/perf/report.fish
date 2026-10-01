#!/usr/bin/env fish
# Usage: scripts/perf/report.fish <label>...
#
# Reads perf-out/<label>.jsonl and .marks from session.fish. Per run: the
# time to the port-forward and to the first list, then per phase the
# requests, KiB, median total_ms and failures by endpoint.

set -l out (realpath (status dirname)/../..)/perf-out

for label in $argv
    set -l marks $out/$label.marks
    if not test -f $marks
        echo "$label: no $marks" >&2
        continue
    end
    set -l start (string split ' ' (grep '^start' $marks))[2]
    set -l detail (string split ' ' (grep '^detail' $marks))[2]
    set -l quit (string split ' ' (grep '^quit' $marks))[2]
    echo "== $label"
    jq -rs --argjson start $start --argjson detail $detail --argjson quit $quit '
      def epoch: capture("(?<s>.*)[.](?<f>[0-9]+)Z") | (.s + "Z" | fromdate) + ("0." + .f | tonumber);
      def median: sort | if length == 0 then 0 else .[(length / 2 | floor)] end;
      def ms: if . == null then "-" else (. * 1000 | round | tostring) + " ms" end;
      map(. + {t: (.time | epoch)}) as $all
      | ($all | map(select(.stage == "forward" and .state == "ready")) | first | .t // null) as $ready
      | ($all | map(select(.endpoint == "list" and .state == "ok")) | first | .t // null) as $first
      | "  forward ready \(if $ready then $ready - $start else null end | ms), first list \(if $first then $first - $start else null end | ms)",
        ( [["list", $start, $detail], ["detail", $detail, $quit]][] as [$phase, $from, $to]
          | ($all | map(select(.stage == "request" and .t >= $from and .t < $to))) as $reqs
          | "  \($phase) (\($to - $from | round) s): \($reqs | length) requests, \((($reqs | map(.bytes // 0) | add) // 0) / 1024 | round) KiB",
            ( $reqs | group_by(.endpoint)[]
              | "    \(.[0].endpoint): \(length) x, \(map(.bytes // 0) | add / 1024 | round) KiB, median \(map(.total_ms // 0) | median | round) ms, failed \(map(select(.state == "failed")) | length)" ) )
    ' $out/$label.jsonl
end

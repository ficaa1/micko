#!/usr/bin/env fish
# Usage: scripts/perf/allns.fish <label> <profile>
#
# Builds the working tree and runs one read-only session that waits for the
# first list, then presses 0 for all namespaces. Prints the time from the
# key press to the list answer, and the /api/v1/info requests before and
# after it.

if test (count $argv) -lt 2
    echo "usage: allns.fish <label> <profile>" >&2
    exit 2
end
set -l label $argv[1]
set -l profile $argv[2]

set -l root (realpath (status dirname)/../..)
set -l out $root/perf-out
set -l bin $out/bin/$label
set -l diag $out/$label.jsonl
set -l sess micko-perf-$label
mkdir -p $out/bin

go -C $root build -o $bin ./cmd/micko; or exit 1

function now
    perl -MTime::HiRes=time -e 'printf "%.3f\n", time'
end

tmux kill-session -t $sess 2>/dev/null
rm -f $diag
tmux new-session -d -s $sess -x 160 -y 45 "$bin --profile $profile --read-only --debug 2> $diag"

for i in (seq 600)
    grep -q '"endpoint":"list"' $diag 2>/dev/null; and break
    sleep 0.1
end
sleep 3

set -l pressed (now)
tmux send-keys -t $sess 0
sleep 8
tmux send-keys -t $sess q
sleep 1
tmux kill-session -t $sess 2>/dev/null

jq -rs --arg label $label --argjson pressed $pressed '
  def epoch: capture("(?<s>.*)[.](?<f>[0-9]+)Z") | (.s + "Z" | fromdate) + ("0." + .f | tonumber);
  map(select(.stage == "request") | . + {t: (.time | epoch)}) as $reqs
  | ($reqs | map(select(.t >= $pressed))) as $after
  | ($after | map(select(.endpoint == "list")) | first) as $list
  | "\($label): info before 0: \($reqs | map(select(.t < $pressed and .endpoint == "info")) | length), info after 0: \($after | map(select(.endpoint == "info")) | length), list answered \(if $list then (($list.t - $pressed) * 1000 | round | tostring) + " ms" else "never" end) after 0 (\($list.state // "-"), list request \($list.total_ms // 0 | round) ms)"
' $diag

#!/usr/bin/env fish
# Usage: scripts/perf/session.fish <label> <profile> <workflow> [list-secs] [detail-secs]
#
# Builds the working tree and runs one scripted, read-only session in tmux:
# the list for list-secs (default 30), then <workflow> in the detail view for
# detail-secs (default 60), then q. Writes perf-out/<label>.jsonl (the
# --debug diagnostics) and perf-out/<label>.marks (phase start times).

if test (count $argv) -lt 3
    echo "usage: session.fish <label> <profile> <workflow> [list-secs] [detail-secs]" >&2
    exit 2
end
set -l label $argv[1]
set -l profile $argv[2]
set -l workflow $argv[3]
set -l list_secs 30
set -l detail_secs 60
test (count $argv) -ge 4; and set list_secs $argv[4]
test (count $argv) -ge 5; and set detail_secs $argv[5]

set -l root (realpath (status dirname)/../..)
set -l out $root/perf-out
set -l bin $out/bin/$label
set -l diag $out/$label.jsonl
set -l marks $out/$label.marks
set -l sess micko-perf-$label
mkdir -p $out/bin

go -C $root build -o $bin ./cmd/micko; or exit 1

function now
    perl -MTime::HiRes=time -e 'printf "%.3f\n", time'
end

tmux kill-session -t $sess 2>/dev/null
rm -f $diag $marks

echo "start "(now) >>$marks
tmux new-session -d -s $sess -x 160 -y 45 "$bin --profile $profile --read-only --debug 2> $diag"

for i in (seq 600)
    grep -q '"endpoint":"list"' $diag 2>/dev/null; and break
    sleep 0.1
end
if not grep -q '"endpoint":"list"' $diag 2>/dev/null
    echo "no list within 60 s; see $diag" >&2
    tmux kill-session -t $sess 2>/dev/null
    exit 1
end
sleep $list_secs

echo "detail "(now) >>$marks
tmux send-keys -t $sess /
sleep 0.3
tmux send-keys -t $sess -l $workflow
sleep 0.3
tmux send-keys -t $sess Enter
sleep 0.5
tmux send-keys -t $sess Enter
sleep 3
tmux capture-pane -p -t $sess >$out/$label.detail.txt
if not grep -q "Detail $workflow" $out/$label.detail.txt
    echo "warning: the detail of $workflow is not on screen; see $out/$label.detail.txt" >&2
end
sleep (math $detail_secs - 3)

echo "quit "(now) >>$marks
tmux send-keys -t $sess q
sleep 2
tmux kill-session -t $sess 2>/dev/null
echo "$label: $diag"

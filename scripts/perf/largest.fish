#!/usr/bin/env fish
# Usage: scripts/perf/largest.fish <kube-context> <namespace> [count]
#
# Lists the workflows of a namespace with the most nodes, the ones that make
# the detail view's cost visible in session.fish. Read only.

if test (count $argv) -lt 2
    echo "usage: largest.fish <kube-context> <namespace> [count]" >&2
    exit 2
end
set -l n 5
test (count $argv) -ge 3; and set n $argv[3]

kubectl --context $argv[1] -n $argv[2] get workflows.argoproj.io -o json \
    | jq -r '.items[] | [(.status.nodes // {} | length), .status.phase, .metadata.name] | @tsv' \
    | sort -rn | head -n $n

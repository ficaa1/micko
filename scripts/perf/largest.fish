#!/usr/bin/env fish
# Usage: scripts/perf/largest.fish <kube-context> <namespace> [count]
#
# Lists the namespace's workflows with the most nodes.

if test (count $argv) -lt 2
    echo "usage: largest.fish <kube-context> <namespace> [count]" >&2
    exit 2
end
set -l n 5
test (count $argv) -ge 3; and set n $argv[3]

kubectl --context $argv[1] -n $argv[2] get workflows.argoproj.io -o json \
    | jq -r '.items[] | [(.status.nodes // {} | length), .status.phase, .metadata.name] | @tsv' \
    | sort -rn | head -n $n

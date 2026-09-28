#!/usr/bin/env bash
# Records real micko screens for the video. Each shot starts the demo fresh
# in a detached tmux session, sends keys, and saves the pane with its colours
# (ANSI SGR) to screens/<name>.ans. Existing files are kept, so a re-run only
# records what is missing; FORCE=1 records everything again.
#
#   shot NAME [--skin S] -- KEY...      one capture after the keys
#   type NAME [--skin S] -- PREP... ++ TEXT
#                                      captures NAME_00 after PREP, then one
#                                      capture per character of TEXT
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(cd .. && pwd)
BIN="$ROOT/dist/micko"
OUT=screens
COLS=${COLS:-124}
ROWS=${ROWS:-32}
mkdir -p "$OUT"
[ -x "$BIN" ] || (cd "$ROOT" && make build)

S=argo-video
start() {
  tmux kill-session -t $S 2>/dev/null || true
  tmux -f /dev/null new-session -d -s $S -x "$COLS" -y "$ROWS" \
    "env COLORTERM=truecolor TERM=xterm-256color $BIN --demo --allow-actions ${EXTRA:-} --skin $1"
  tmux set -t $S default-terminal tmux-256color >/dev/null
  tmux set -ga terminal-overrides ",*:RGB" >/dev/null
  sleep 2
}
key() { tmux send-keys -t $S "$1"; sleep 0.35; }
lit() { tmux send-keys -t $S -l "$1"; sleep 0.25; }
save() { sleep 0.9; tmux capture-pane -t $S -e -p > "$OUT/$1.ans"; echo "  $1"; }
want() { [ "${FORCE:-0}" = 1 ] || [ ! -f "$OUT/$1.ans" ]; }

shot() {
  local name=$1; shift; local skin=${SKIN:-monokai}
  if [ "$1" = --skin ]; then skin=$2; shift 2; fi
  [ "$1" = -- ] && shift
  want "$name" || return 0
  start "$skin"; for k in "$@"; do key "$k"; done; save "$name"
}

type_shot() {
  local name=$1; shift; local skin=${SKIN:-monokai}
  if [ "$1" = --skin ]; then skin=$2; shift 2; fi
  [ "$1" = -- ] && shift
  want "${name}_00" || return 0
  start "$skin"
  while [ "$1" != ++ ]; do key "$1"; shift; done; shift
  save "${name}_00"
  local text=$1 i
  for ((i = 0; i < ${#text}; i++)); do
    lit "${text:$i:1}"; save "$(printf '%s_%02d' "$name" $((i + 1)))"
  done
}

echo "recording into video/$OUT"
# Workflow list, cursor walking down.
shot list_0 --
shot list_1 -- Down
shot list_2 -- Down Down
shot list_3 -- Down Down Down
shot list_wide -- w
shot allns -- 0
# Detail sections of the failed nightly report (DAG with retries + exit handler).
shot nightly_timeline -- Down Down T
shot nightly_nodes -- Down Down Enter 2
shot nightly_explain -- Down Down X
shot nightly_logs -- Down Down l
# The running training fan-out: timeline with a now line, node info panel.
shot train_timeline -- Down Down Down Down Down T
shot train_nodes -- Down Down Down Down Down Enter 2
shot train_info -- Down Down Down Down Down Enter 2 Down Down Down Down i
# Hourly ETL: short explain card with quoted log lines.
shot etl_explain -- Down X
shot gate_events -- E
shot help -- ?
# Command palette typing ":cron", then the cron list and its info panel.
type_shot palette -- : ++ cron
shot cron_list -- : c r o n Enter
shot cron_info -- : c r o n Enter i
# Query-language filter typed live.
type_shot filter -- / ++ "phase=Failed age<3h"
# Marks.
shot marks_1 -- Down Space
shot marks_2 -- Down Space Down Space
shot marks_3 -- Down Space Down Space Down Space
# Every truecolor skin on the same list screen.
for skin in catppuccin-mocha catppuccin-latte gruvbox-dark gruvbox-light nord dracula \
  tokyo-night solarized-dark solarized-light one-dark rose-pine rose-pine-dawn monokai; do
  shot "skin_$skin" --skin "$skin" -- Down Down
done
# Mićko, the mascot, perches on the pane from 80x40 up: 40-row shots.
ROWS=40 EXTRA=--mascot
shot mascot_list --
shot mascot_timeline -- Down Down T
for skin in catppuccin-latte gruvbox-dark dracula rose-pine-dawn nord gruvbox-light; do
  shot "mascot_skin_$skin" --skin "$skin" --
done
tmux kill-session -t $S 2>/dev/null || true
echo done

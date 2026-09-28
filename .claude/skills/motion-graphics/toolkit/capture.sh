#!/usr/bin/env bash
# Records real terminal screens of your app for one video:
#
#   bash lib/capture.sh VIDEO          record what videos/VIDEO/screens lacks
#   FORCE=1 bash lib/capture.sh VIDEO  record everything again
#
# The shot list is videos/VIDEO/shots.sh, which calls the functions below.
# Each shot starts the demo fresh in a detached tmux session, sends keys and
# saves the pane with its colours (ANSI SGR) to screens/NAME.ans, with the skin
# in screens/NAME.meta. Afterwards lib/ansi2json.mjs builds videos/VIDEO/screens.js.
#
#   shot NAME [--skin S] -- KEY...        one capture after the keys
#   type_shot NAME [--skin S] -- PREP... ++ TEXT
#                                         NAME_00 after PREP, then one capture per
#                                         character of TEXT (NAME_01, NAME_02, ...)
#   anim NAME [--skin S] -- KEY... ++ SECONDS
#                                         after the keys, capture 10 times a second
#                                         for SECONDS; distinct screens are kept as
#                                         NAME_000, NAME_001, ... and NAME.seq lists
#                                         "time frame" for every capture, so a scene
#                                         can replay an animation the app draws.
#
# Shot lists set BIN (the app to run, required) and may set: ARGS (its
# arguments, e.g. a demo mode), SKIN (a theme name passed as `--skin NAME`
# when SKIN_FLAG is set, e.g. SKIN_FLAG=--theme; recorded either way so
# ansi2json knows the screen's default colours), EXTRA (more flags),
# COLS/ROWS (terminal size, default 124x32). Every run gets an empty HOME, so
# no real config or history is read or written.
set -euo pipefail
VIDEO=${1:?usage: bash lib/capture.sh VIDEO}
cd "$(dirname "$0")/.."
VDIR="videos/$VIDEO"
[ -f "$VDIR/shots.sh" ] || { echo "no $VDIR/shots.sh"; exit 1; }
OUT="$VDIR/screens"
COLS=${COLS:-124}
ROWS=${ROWS:-32}
SKIN=${SKIN:-default}
SKIN_FLAG=${SKIN_FLAG:-}
EXTRA=${EXTRA:-}
ARGS=${ARGS:-}
# An empty home, so no real config file or journal is ever read or written.
FAKEHOME=$(mktemp -d)
mkdir -p "$OUT"

S=mg-capture
start() {
  [ -n "${BIN:-}" ] || { echo "shots.sh must set BIN (the app to record)"; exit 1; }
  tmux kill-session -t $S 2>/dev/null || true
  tmux -f /dev/null new-session -d -s $S -x "$COLS" -y "$ROWS" \
    "env HOME=$FAKEHOME XDG_CONFIG_HOME=$FAKEHOME/.config COLORTERM=truecolor TERM=xterm-256color $BIN $ARGS $EXTRA ${SKIN_FLAG:+$SKIN_FLAG $1}"
  tmux set -t $S default-terminal tmux-256color >/dev/null
  tmux set -ga terminal-overrides ",*:RGB" >/dev/null
  sleep 2
}
key() { tmux send-keys -t $S "$1"; sleep 0.35; }
lit() { tmux send-keys -t $S -l "$1"; sleep 0.25; }
grab() { tmux capture-pane -t $S -e -p; }
save() { sleep 0.9; grab > "$OUT/$1.ans"; echo "skin=$CUR_SKIN" > "$OUT/$1.meta"; echo "  $1"; }
want() { [ "${FORCE:-0}" = 1 ] || [ ! -f "$OUT/$1.ans" ]; }
args() { # parse "[--skin S] --" into CUR_SKIN; sets REST
  CUR_SKIN=$SKIN
  if [ "${1:-}" = --skin ]; then CUR_SKIN=$2; shift 2; fi
  [ "${1:-}" = -- ] && shift
  REST=("$@")
}

shot() {
  local name=$1; shift; args "$@"
  want "$name" || return 0
  start "$CUR_SKIN"; for k in "${REST[@]}"; do key "$k"; done; save "$name"
}

type_shot() {
  local name=$1; shift; args "$@"
  want "${name}_00" || return 0
  start "$CUR_SKIN"
  set -- "${REST[@]}"
  while [ "$1" != ++ ]; do key "$1"; shift; done; shift
  save "${name}_00"
  local text=$1 i
  for ((i = 0; i < ${#text}; i++)); do
    lit "${text:$i:1}"; save "$(printf '%s_%02d' "$name" $((i + 1)))"
  done
}

anim() {
  local name=$1; shift; args "$@"
  want "${name}_000" || return 0
  rm -f "$OUT/${name}"_[0-9][0-9][0-9].ans "$OUT/${name}"_[0-9][0-9][0-9].meta "$OUT/$name.seq"
  start "$CUR_SKIN"
  set -- "${REST[@]}"
  while [ "$1" != ++ ]; do key "$1"; shift; done; shift
  local secs=$1 t0 now n=0 last="" frame tmp
  declare -A seen=()
  tmp=$(mktemp)
  t0=$(date +%s.%N)
  while :; do
    now=$(date +%s.%N)
    awk -v a="$now" -v b="$t0" -v s="$secs" 'BEGIN { exit !(a - b < s) }' || break
    grab > "$tmp"
    local h; h=$(md5sum < "$tmp" | cut -c1-16)
    if [ -z "${seen[$h]:-}" ]; then
      frame=$(printf '%s_%03d' "$name" $n); n=$((n + 1)); seen[$h]=$frame
      cp "$tmp" "$OUT/$frame.ans"; echo "skin=$CUR_SKIN" > "$OUT/$frame.meta"
    fi
    frame=${seen[$h]}
    [ "$frame" != "$last" ] && awk -v a="$now" -v b="$t0" -v f="$frame" 'BEGIN { printf "%.2f %s\n", a - b, f }' >> "$OUT/$name.seq"
    last=$frame
    sleep 0.1
  done
  rm -f "$tmp"
  echo "  $name: $n distinct frames over ${secs}s"
}

# shellcheck source=/dev/null
source "$VDIR/shots.sh"
tmux kill-session -t $S 2>/dev/null || true
node lib/ansi2json.mjs "$VIDEO"

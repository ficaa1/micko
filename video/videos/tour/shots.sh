# Shot list for the tour video (sourced by lib/capture.sh). Recorded with the
# 0.7.0 binary: `make build` at the repo root, then `bash lib/capture.sh tour`.
SKIN=monokai

# Workflow list, cursor walking down.
shot list_0 --
shot list_1 -- Down
shot list_2 -- Down Down
shot list_3 -- Down Down Down
# Detail sections of the failed nightly report (DAG with retries + exit handler).
shot nightly_timeline -- Down Down T
shot nightly_nodes -- Down Down Enter 2
shot nightly_explain -- Down Down X
shot nightly_logs -- Down Down l
# The running training fan-out: timeline with a now line, node info panel.
shot train_timeline -- Down Down Down Down Down T
shot train_info -- Down Down Down Down Down Enter 2 Down Down Down Down i
shot gate_events -- E
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
# The profile picker, with sample profiles instead of the demo.
ARGS="--config $PWD/videos/tour/fixtures/config.yaml"
shot picker --
ARGS="--demo --allow-actions"
# Mićko's 0.7 routines (about 45 s a pass): perched on the list, and on the floor.
ROWS=40 EXTRA=--mascot
anim perch -- ++ 42
EXTRA=--mascot=floor
anim floor -- ++ 42

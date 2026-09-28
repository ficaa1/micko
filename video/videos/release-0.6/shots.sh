# Shot list for the 0.6 release video (sourced by lib/capture.sh).
# Recorded with the 0.6.0 binary: to re-record, build tag v0.6.0 and pass it,
#   git worktree add /tmp/micko-0.6 v0.6.0 && (cd /tmp/micko-0.6 && make build)
#   BIN=/tmp/micko-0.6/dist/micko FORCE=1 bash lib/capture.sh release-0.6
SKIN=monokai

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

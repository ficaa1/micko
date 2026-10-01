#!/usr/bin/env fish
# Usage: site/capture.fish [name...]
#
# Records the terminal screens the site shows into site/screens/NAME.ans,
# then rebuilds site/screens.js. Each shot starts `micko --demo` fresh in a
# detached tmux session with an empty HOME, sends keys and saves the pane
# with its colours. With names, only those shots are recorded.
# Needs tmux, node and dist/micko (make build).

set -l root (path resolve (status dirname)/..)
set -g bin $root/dist/micko
set -g out $root/site/screens
set -g only $argv
set -g sess micko-site
set -g home (mktemp -d)

test -x $bin; or begin
    echo "no $bin: run make build" >&2
    exit 1
end

function start -a skin rows extra
    tmux kill-session -t $sess 2>/dev/null
    tmux -f /dev/null new-session -d -s $sess -x 124 -y $rows \
        "env HOME=$home XDG_CONFIG_HOME=$home/.config COLORTERM=truecolor TERM=xterm-256color $bin --demo $extra --skin $skin"
    tmux set -t $sess default-terminal tmux-256color >/dev/null
    tmux set -ga terminal-overrides ",*:RGB" >/dev/null
    sleep 2
end

function keys
    for k in $argv
        tmux send-keys -t $sess $k
        sleep 0.35
    end
end

function save -a name
    sleep 0.9
    tmux capture-pane -t $sess -e -p >$out/$name.ans
    echo "  $name"
end

function wanted -a name
    test (count $only) -eq 0; or contains -- $name $only
end

# shot NAME SKIN KEY...: one capture after the keys.
function shot -a name skin
    wanted $name; or return 0
    start $skin 32 ''
    keys $argv[3..]
    save $name
end

# typed NAME TEXT KEY...: NAME_00 after the keys, then one capture per
# character of TEXT.
function typed -a name text
    wanted $name; or return 0
    start monokai 32 ''
    keys $argv[3..]
    save {$name}_00
    for i in (seq (string length -- $text))
        tmux send-keys -t $sess -l (string sub -s $i -l 1 -- $text)
        sleep 0.25
        save (printf '%s_%02d' $name $i)
    end
end

# anim NAME SECONDS FLAG: Mićko's routine, captured ten times a second. Distinct
# screens are kept as NAME_000, NAME_001, ...; NAME.seq lists "seconds frame"
# at every change.
function anim -a name secs flag
    wanted $name; or return 0
    set -l old $out/{$name}_*.ans
    rm -f $old $out/$name.seq
    start monokai 40 $flag
    set -l t0 (perl -MTime::HiRes=time -e 'print time')
    set -l hashes
    set -l last ''
    while true
        set -l now (perl -MTime::HiRes=time -e 'print time')
        set -l t (math "$now - $t0")
        test $t -lt $secs; or break
        set -l screen (tmux capture-pane -t $sess -e -p | string collect)
        set -l h (printf '%s' $screen | md5 -q)
        set -l n (contains -i -- $h $hashes)
        if test -z "$n"
            set -a hashes $h
            set n (count $hashes)
            printf '%s\n' $screen >$out/(printf '%s_%03d' $name (math $n - 1)).ans
        end
        set -l frame (printf '%s_%03d' $name (math $n - 1))
        if test $frame != $last
            printf '%.2f %s\n' $t $frame >>$out/$name.seq
            set last $frame
        end
        sleep 0.1
    end
    echo "  $name: "(count $hashes)" frames over $secs"s
end

mkdir -p $out
echo "recording with "($bin --version)

shot list monokai Down Down
shot timeline monokai Down Down T
shot explain monokai Down Down X
shot nodes monokai Down Down Enter
shot logs monokai Down Down l
shot events monokai E
shot cron monokai : c r o n Enter
shot marks monokai Down Space Down Space Down Space
typed filter 'phase=Failed age<3h' /
for skin in catppuccin-mocha catppuccin-latte gruvbox-dark gruvbox-light nord dracula \
        tokyo-night solarized-dark solarized-light one-dark rose-pine rose-pine-dawn monokai
    shot skin_$skin $skin Down Down
end
anim perch 42 --mascot

tmux kill-session -t $sess 2>/dev/null
rm -rf $home
node $root/site/screens.mjs

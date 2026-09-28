# Shot list for this video (sourced by lib/capture.sh VIDEO).
# Each shot starts the app fresh in tmux, sends keys, and saves the screen.
BIN=${BIN:-$HOME/go/bin/yourapp}   # the app to record; make it a demo/offline mode if it has one
ARGS="--demo"                      # its arguments
SKIN=default                       # a name from skins.json (the terminal's default colours)
# SKIN_FLAG=--theme                # if the app takes a theme flag, SKIN is passed with it

shot main --                       # the first screen, no keys
shot main_down -- Down Down        # after pressing Down twice (tmux key names: Enter, Escape, Space, C-c, ...)
type_shot search -- / ++ "error"   # search_00 after "/", then one screen per typed character
# anim spinner -- ++ 10            # 10 s at 10 fps: distinct frames + timings, for animations the app draws

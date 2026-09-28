#!/usr/bin/env bash
# Sets up a motion graphics project, or adds a video to an existing one:
#   bash scaffold.sh DIR NAME      e.g. bash scaffold.sh video tour
# DIR gets lib/ (the toolkit), package.json, skins.json, PROGRESS.md, assets/,
# renders/; DIR/videos/NAME gets index.html, two starter scenes and shots.sh.
# Existing files are never overwritten, so re-running only adds what is missing.
set -euo pipefail
DIR=${1:?usage: scaffold.sh DIR NAME}; NAME=${2:?usage: scaffold.sh DIR NAME}
SKILL=$(cd "$(dirname "$0")/.." && pwd)
T="$SKILL/templates"
put() { [ -e "$2" ] && echo "  keep $2" || { mkdir -p "$(dirname "$2")"; cp -r "$1" "$2"; echo "  add  $2"; }; }
mkdir -p "$DIR"
if [ -d "$DIR/lib" ]; then echo "  keep $DIR/lib (compare with $SKILL/toolkit if it is older)"; else cp -r "$SKILL/toolkit" "$DIR/lib"; echo "  add  $DIR/lib"; fi
put "$T/package.json" "$DIR/package.json"
put "$T/gitignore" "$DIR/.gitignore"
put "$T/skins.json" "$DIR/skins.json"
put "$T/PROGRESS.md" "$DIR/PROGRESS.md"
mkdir -p "$DIR/assets" "$DIR/renders"
V="$DIR/videos/$NAME"
put "$T/shots.sh" "$V/shots.sh"
put "$T/scenes" "$V/scenes"
if [ ! -e "$V/index.html" ]; then sed "s/VIDEO_NAME/$NAME/g" "$T/index.html" > "$V/index.html"; echo "  add  $V/index.html"; fi
sed -i "s/VIDEO_NAME/$NAME/g" "$V/scenes/"*.js
cat <<MSG

Next:
  cd $DIR && npm install && npx playwright install chromium
  edit videos/$NAME/shots.sh (BIN, ARGS, shots), then: bash lib/capture.sh $NAME
  edit videos/$NAME/scenes/*, check: node lib/render.mjs $NAME --still title:2
MSG

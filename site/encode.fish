#!/usr/bin/env fish
# Usage: site/encode.fish <render.mp4> [poster-seconds]
#
# Encodes a finished video render for the page: AV1 for browsers that decode
# it, H.264 for the rest, both with AAC audio and the index up front so they
# start playing before the download ends. Also writes the poster frame.

set -l src $argv[1]
set -l at 4.6
test (count $argv) -ge 2; and set at $argv[2]
test -f "$src"; or begin
    echo "usage: site/encode.fish <render.mp4> [poster-seconds]" >&2
    exit 1
end
set -l media (path resolve (status dirname))/media
mkdir -p $media

ffmpeg -y -v error -i $src -c:v libsvtav1 -preset 5 -crf 46 -pix_fmt yuv420p \
    -c:a aac -b:a 96k -movflags +faststart $media/tour-av1.mp4; or exit 1
ffmpeg -y -v error -i $src -c:v libx264 -preset slow -crf 30 -tune animation -pix_fmt yuv420p \
    -c:a aac -b:a 96k -movflags +faststart $media/tour-h264.mp4; or exit 1
ffmpeg -y -v error -ss $at -i $src -frames:v 1 -q:v 4 $media/poster.jpg; or exit 1
ls -l $media

# The README recording. Run it from the repository root:
#
#     make build && vhs docs/demo.tape
#
# It records the demo dataset, so it needs no cluster and produces the same
# frames on every machine. Keep it under 30 seconds: the list, one failed
# run's timeline and explanation, the filter, the palette, and Mićko for a
# moment at the end.
#
# Mićko needs a terminal of at least 80x40: at FontSize 15 a row is about
# 20.6 pixels, and Height 880 gives 41 rows.

Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1100
Set Height 880
Set Padding 18
Set TypingSpeed 50ms
Set Framerate 15
Set PlaybackSpeed 1.0

# The build is not part of the recording.
Hide
Type "PS1='$ '" Enter
Type "clear" Enter
Show

Type "./dist/micko --demo" Sleep 300ms Enter
Sleep 2s

# Every run at a glance: phase as a glyph, a word and a colour.
Down Sleep 400ms
Down Sleep 700ms

# Open the failed run straight on its timeline, then ask why it failed.
Type "T" Sleep 2.5s
Type "X" Sleep 3s
Escape Sleep 600ms

# The filter is a small query language.
Type "/" Sleep 300ms
Type "phase=failed age<3h" Sleep 700ms
Enter Sleep 1.6s
Escape Sleep 500ms

# The palette switches what the pane lists.
Type ":" Sleep 300ms
Type "cron" Sleep 400ms Enter
Sleep 1s
Type "i" Sleep 2s
Type ":" Sleep 300ms
Type "wf" Sleep 300ms Enter
Sleep 700ms

# And Mićko, off by default, perches on the pane.
Type ":" Sleep 300ms
Type "mascot" Sleep 400ms Enter
Sleep 2.5s

Type "q"
Sleep 600ms

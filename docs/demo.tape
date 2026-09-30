# The README recording. Run it from the repository root:
#
#     make build && vhs docs/demo.tape
#
# It records the demo dataset, so it needs no cluster and produces the same
# frames on every machine. It follows the README: the list, one failed run's
# timeline and explanation, the filter, the palette, and Mićko last.
#
# Mićko needs a terminal of at least 80x40: at FontSize 15 a row is about
# 20.6 pixels, and Height 880 gives 41 rows.
#
# His routines play in real time and take about 40 seconds a pass, so the
# waits between his moves are hidden. The Hide/Show pairs below are timed
# against MickoPerchBeats and MickoFloorBeats in internal/ui/shared: he looks
# around about 32 seconds after he perches and turns to his mirror about 32
# seconds after he sits on the floor.

Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1100
Set Height 880
Set Padding 18
Set TypingSpeed 55ms
Set Framerate 15
Set PlaybackSpeed 1.0

# The build is not part of the recording.
Hide
Type "PS1='$ '" Enter
Type "clear" Enter
Show

Type "./dist/micko --demo" Sleep 400ms Enter
Sleep 2.5s

# Every run at a glance: phase as a glyph, a word and a colour.
Down Sleep 500ms
Down Sleep 900ms

# Open the failed run straight on its timeline, then ask why it failed.
Type "T" Sleep 3s
Type "X" Sleep 3.5s
Escape Sleep 900ms

# The filter is a small query language.
Type "/" Sleep 400ms
Type "phase=failed age<3h" Sleep 1s
Enter Sleep 2s
Escape Sleep 800ms

# The palette switches what the pane lists.
Type ":" Sleep 400ms
Type "cron" Sleep 600ms Enter
Sleep 1.5s
Type "i" Sleep 2.5s
Type ":" Sleep 400ms
Type "wf" Sleep 500ms Enter
Sleep 1s

# Mićko perches on the pane, then, after a cut, looks left and right.
Type ":" Sleep 400ms
Type "mascot" Sleep 600ms Enter
Sleep 3s
Hide
Sleep 28.5s
Show
Sleep 8s

# A second :mascot sits him on the floor; after a cut he hops to his mirror.
Type ":" Sleep 400ms
Type "mascot" Sleep 600ms Enter
Sleep 2.5s
Hide
Sleep 28.5s
Show
Sleep 12.5s

Type "q"
Sleep 1s

# The README recording. Run it from the repository root:
#
#     make build && vhs docs/demo.tape
#
# It records the demo dataset, so it needs no cluster and produces the same
# frames on every machine. Keep it short: the file is committed, and a reader
# who wants more presses a key themselves.
#
# Mićko is on, so the terminal must stay at least 80x40: at FontSize 15 a row
# is about 20.6 pixels, and Height 880 gives 41 rows.

Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1100
Set Height 880
Set Padding 18
Set TypingSpeed 55ms
Set Framerate 12
Set PlaybackSpeed 1.0

# The build is not part of the recording.
Hide
Type "PS1='$ '" Enter
Type "clear" Enter
Show

Type "./dist/micko --demo --mascot" Sleep 400ms Enter
Sleep 3s

# Move down the list; the phase column carries a symbol, a word and a colour.
Down Sleep 500ms
Down Sleep 500ms
Down Sleep 900ms

# Open a workflow, then walk its sections.
Enter Sleep 2s
Type "j" Sleep 400ms
Type "j" Sleep 900ms
Tab Sleep 1.8s
Tab Sleep 1.8s
Escape Sleep 900ms

# Filter the list by phase.
Type "p" Sleep 1.2s
Type "p" Sleep 1.2s
Escape Sleep 800ms

# The key map is one keystroke away on every screen.
Type "?" Sleep 2.5s
Escape Sleep 600ms

# The palette moves Mićko from his perch to the floor, beside his mirror.
Type ":" Sleep 500ms
Type "mascot" Sleep 700ms Enter
Sleep 4s

Type "q"
Sleep 1.5s

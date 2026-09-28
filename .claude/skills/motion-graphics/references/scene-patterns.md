# Scene patterns

Proven shapes from the micko videos. The full scenes live in the micko repo at
`video/videos/tour/scenes/` and `video/videos/release-0.6/scenes/`; the file
named after each pattern is the one to copy from.

## Contents
- Headline + terminal + callouts (`03-list.js`)
- Chart that draws itself, then callouts one by one (`04-timeline.js`)
- Camera walk through a long screen (`05-explain.js`)
- Typing into the app with keycaps (`08-palette.js`)
- Big typed query over the live result (`09-filter.js`)
- Cards that explain jobs, with thumbnails of real screens (`02-what.js`)
- Designed cards for things the demo cannot show (`10-actions.js`, `11-connect.js`)
- Wall of themes (`12-skins.js`)
- Photo collage with tape (release-0.6 `03-bird.js`)
- Close-up lens replaying a recorded animation (`13-micko.js`)
- Title and outro (`01-intro.js`, `14-outro.js`)

## Headline + terminal + callouts

Headline on one side (x 110, width ~600), the window on the other at scale
~0.93 centred around X 1300. The window swings in (`ry` from -35 to 0 with
`persp`), the screen advances through captures (`term.show`), then one region
at a time gets `E.hl` + `E.spot` + a pill underneath. Two or three callouts per
scene at most; each needs about 1.2 s on screen to be read.

## Chart that draws itself

Cover the chart area with a div in the skin's background colour and slide its
left edge to the right (`wipe` cover), with a thin glowing edge line. Then
callouts. To switch to a second screen, use `term.wipe(a, b, p)`: crossfading
two terminal screens mixes their text into noise.

## Camera walk through a long screen

`E.camPath(t, [[2.2, wide], [2.95, k1], [3.95, k1], [4.55, k2], ...])` with
keyframes that hold (same camera twice) while a callout is read. Zoom 1.5-1.6.
Pills go where there is empty space on the screen, or in a lower third
(Y ~975, centred) when the screen is full.

## Typing into the app

Record with `type_shot NAME -- PREP ++ TEXT`: one capture per character.
Show `NAME_nn` by time, and put keycaps (chips with a thick bottom shadow)
under the window that pop and press at the same times. Add
`sfx: E.typing(...)` with matching times.

## Big typed query over the live result

The query in huge mono type above the window, coloured by token, with a block
cursor; the window below shows the real screen for the same number of
characters. Read a number from the capture (e.g. "N shown" in the footer) and
show it big beside the window, so the result is legible at video size.

## Cards with thumbnails

A row of 3-4 `.card`s, each with an `E.term` inside at scale ~0.3 as a
thumbnail, a big verb, key chips, one line. Cards drop in with a stagger, then
lift one at a time. Good as the "what it does" overview before any detail.

## Designed cards for what the demo cannot show

When the app cannot show something in a demo (writes, auth, a real cluster),
draw it as designed cards (menu rows, outcome states, facts) next to the real
screen that leads to it. Say only what the docs say.

## Wall of themes

Record the same screen once per theme (`--skin NAME`), lay the windows out
small (scale ~0.36) in two rows inside a container with
`perspective(2000px) rotateY(-16deg) rotateX(8deg)` and pan it sideways. Each
tile flips in (rotateY 80 -> 0) with a stagger.

## Photo collage

Prints: cream border, ink outline, hard shadow (`0 0 0 2.5px ink, 9px 9px 0
2.5px ink`), washi tape strips (`.tape`) at the corners, rotations of a few
degrees. Drop them in one by one (`sfx` thump each), then bring one forward
while the rest go grey (`filter: grayscale() opacity()`, not blur, on paper).

## Close-up lens replaying a recorded animation

`anim NAME -- keys ++ 42` records distinct frames `NAME_000...` and their
timing. For a close-up, put the whole `E.term` inside a box with
`overflow: hidden`, ink border and hard shadow, and use `E.cam` to centre the
region at scale ~2. Find the region from the data (below). Play the frames in
the app's own order but faster than life (a 45 s loop in 4 s): hold the rest
pose, then each distinctive pose for 0.3-0.7 s, blinks for 0.12-0.15 s.

## Find it in the data

Compute coordinates from `window.SCREENS[name].rows` instead of typing them:
the row that holds the pane's top border (`r.includes('╭')`), the bottom
border (`r[0] === '╰'`; a mirror or box inside the pane may also contain
`╰`), the bounding box of non-blank cells in a region across all frames of a
recording. Scenes then survive a re-recording.

## Title and outro

Title: the prompt types the command, the wordmark drops in letter by letter
(off-register print via `.grad`), a tagline word by word, then glyph chips
(the app's own status glyphs), then everything pushes toward the camera.
Outro: wordmark (+ an avatar if there is a mascot or logo), one line of what
it is, a card that types the install and try commands, a ticker of features
in a masked band, fade to the ink colour. Keep version numbers out of
evergreen videos.

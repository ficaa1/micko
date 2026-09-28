# Lessons (things that went wrong once)

## Look
- Without a direction, the look drifts to the same place every time: dark navy
  gradient, blurred colour blobs, perspective grid, blue-to-purple gradient
  text, Space Grotesk. Users notice when two people's videos look alike. Derive
  the palette from the subject (a logo, a mascot, the product's own theme), pick
  a texture on purpose, and write the reasons in PROGRESS.md.
- The terminal theme decides how "punchy" the screens read. A light theme on a
  light background blends in; muted inks look muddy on video. Show the owner
  the same screen in 5-6 themes on the real background (one still) and let them
  choose; it takes a minute and saves a full render.
- Monospace everywhere suits a terminal tool, but it is wider: text that fit in
  a proportional face will overflow. Re-check every card and menu after a font
  change.

## Rendering
- Every frame is a Chromium layout + PNG screenshot: ~1.5-3 s per frame per
  job on 4 cores; 90 s of video took ~30 min. It is CPU-bound; a GPU helps
  little. Use `--jobs` about cores/2, re-render only changed scenes, and review
  with stills and contact sheets before any full render.
- Scenes must wait for images (`img.decode()`) before the first frame, or the
  first frames show empty photos. The engine does this in `E.renderAt`.
- `E.set` only writes `transform` when a transform key is given; a bare
  `E.set(n, {o})` must not wipe a transform set in CSS or elsewhere.
- Numbers passed to `E.css` get `px` appended, except opacity, zIndex,
  lineHeight, fontWeight and flex. A `lineHeight: 1.4` that became `1.4px`
  stacked every wrapped line on top of the first.
- The scene root's inline style must be reset between scenes (the engine does),
  or a fade-out on one scene leaks into the next.

## Terminal captures
- Record the real app in tmux with truecolor (`COLORTERM=truecolor`,
  `terminal-overrides ",*:RGB"`); `capture-pane -e` keeps the colours. The app
  never paints the default background and foreground, so each theme's bg/text
  must come from `skins.json`.
- Draw block elements (█▌▐░▒▓ and the eighths) as CSS cells, not font glyphs,
  or bars show seams between cells at any scale. The engine does this.
- Glyphs the mono font lacks (✓ ✗ ◐ ◆ …) go in 1ch-wide inline blocks so the
  grid stays aligned.
- Fontsource ships latin and latin-ext as separate files; declare both with
  `unicode-range`, or a name like "Mićko" renders its "ć" in a fallback face.
- Clock-dependent text (ages, timestamps) changes between recordings; callout
  coordinates do not. Diff old and new captures (ignoring SGR) after
  re-recording to be sure nothing moved.
- Give every run an empty HOME so no real config or history leaks into a
  capture. For screens that need config (a profile picker), use a fixture file.
- For a loop the app animates (a mascot, a spinner), record ~one full loop at
  10 fps and keep distinct frames plus timings (`anim`), rather than guessing
  times; loops with fixed hold times record reliably.

## Delivery
- Commit drafts under their own names (`renders/NAME-draft.mp4`); renders write
  a new file, so an iteration never overwrites what the owner already saw.
- Uploads to the user may be capped (30 MiB here). Paper grain and noise cost
  bitrate; make a lighter preview encode (`-crf 26 -tune grain`) to send and
  keep the full-quality file in the repo.
- The synthesized soundtrack is a placeholder; owners usually bring their
  music. `--music FILE` or `videos/NAME/music.mp3` trims and fades it; the
  cut times (scene starts) help them sync.
- Write the plan and every decision into PROGRESS.md and commit after each
  step: long renders and usage limits interrupt sessions.

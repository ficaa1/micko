# Motion graphic: micko 0.6 — progress log

This file is the hand-off note. If a session is cut off, a new one should read
this file first, then continue from the first unchecked item. Every step is
committed on its own, so `git log -- video/` shows where things stopped.

The project was argo-tui until 0.6. The 0.5 video (`argo-tui-0.5.mp4`) is
kept as it was released; the source now builds the 0.6 video.
0.6 renames the tool to micko after Mićko, the owner's eastern rosella, and
adds him as an opt-in mascot (`--mascot`) that perches on the pane at 80x40+.

## Where things stand

- **The 0.6 draft is kept** as `micko-0.6-draft.mp4` (+ `micko-0.6-draft-poster.png`).
  Renders write `micko-0.6.mp4` by default (or `--out NAME.mp4`), so they
  never overwrite the draft. The commit that rendered it is tagged in history
  as "video: render the micko 0.6 draft".
- **Next:** refinements before shipping, rendered on the owner's own PC, with
  the owner's own music (see below). The synthesized soundtrack was only for
  the draft.

## Render on your own PC

Needs Node 22+ and nothing else: the screens are committed, so Go, tmux and
the capture step are only needed if the UI changes (capture is bash + tmux, so
on Windows run it under WSL; rendering works natively).

```sh
git checkout claude/argo-tui-motion-graphic-lpmtsz
cd video
npm install
npx playwright install chromium          # once: the browser that takes the frames
node render.mjs --jobs 8                 # ~ one job per 2 CPU cores
```

- Speed comes from CPU cores: each frame is a Chromium layout + screenshot,
  mostly CPU-bound. The draft took ~30 min on 4 cores; a 16-core desktop with
  `--jobs 8` should be several times faster. The GPU helps little.
- Only changed scenes re-render (per-scene mp4s in `out/scenes/`). A fresh
  clone has no `out/`, so its first render does every scene.
- **Your own music:** drop the track at `audio/music.mp3` (or `.m4a/.wav/.flac/.ogg`),
  or pass `--music path/to/song.mp3`. It is trimmed to the video with a 2 s
  fade-out and replaces the synthesized soundtrack. Changing music only
  re-joins; no scene re-renders. Cuts in the draft, for syncing music
  (from each scene's `dur:`): release 7.0 s, bird 11.5, mascot 19.5, list 26.5,
  timeline 33.5, explain 41.5, nodes 49.0, logs 55.5, palette 62.0, filter 68.5,
  skins 74.0, actions 81.0, outro 87.5, end 94.5.
- Iterate on one scene: open `src/index.html` in a browser (scrubber), or
  `node render.mjs --still bird:3.5`, then `node render.mjs bird` to re-render it.

## Resume in one command

```sh
cd video && npm install && npm run all   # capture (if screens missing) + render + encode
```

`npm run render` skips any scene whose `out/scenes/<id>.mp4` is newer than its
source and the shared engine, so a re-run only redoes what changed.

## v0.6 draft: steps

- [x] 1. Merge `main` (the rename) into this branch; `make build` gives `dist/micko`
- [x] 2. Photos of Mićko into `src/photos/` (resized, EXIF stripped)
- [x] 3. Recapture every screen with `dist/micko` (`FORCE=1 npm run capture`);
      add 40-row `--mascot` shots; check callout coordinates still line up
- [x] 4. Rename in scenes: intro retypes `argo-tui` as `micko`, release rolls to 0.6,
      window titles, outro commands
- [x] 5. New scenes: `bird` (photos) and `mascot` (the ASCII Mićko perched, across skins)
- [x] 6. Soundtrack re-timed; full render (94.5 s, ~22 MB, draft sent for review) -> kept as `micko-0.6-draft.mp4`
- [ ] 7. Restyle "Mićko on paper" (owner's direction): see the section below
- [ ] 8. Refinements (owner's list), owner's music, final render on the owner's PC

## Restyle: Mićko on paper

Direction from the owner: lean into Mićko's colours, paper textures, TUI-type
presentation; terminal skin gruvbox or similar. Decisions:
- Palette from his plumage, printed like risograph ink on warm paper:
  crimson `#d7263d` (head, chest), cobalt `#3f5bb8` and violet-blue `#6f7fd6`
  (wing), yellow `#f2c14e` / `#e3a21a` (scallop edges), olive `#6f8a2a`,
  cheek cream `#fbf8f1`; ink `#1f1a17` on paper `#efe4cf`.
- Background: paper with grain that "boils" on twos, faint engineering-grid
  lines, soft mottling; no glows, no dark gradients.
- Type: JetBrains Mono throughout (TUI); kickers as box-drawing rules
  (`╭─ KICKER ──`), titles with a blinking block cursor; misregistered
  crimson/cobalt print offset on the wordmark and the big numbers.
- Objects: cards, pills, keycaps and windows as paper cut-outs with ink
  borders and hard offset shadows; photos taped on with washi tape.
- Terminal skin: gruvbox-light (its `#fbf1c7` reads as a sheet of paper);
  every screen recaptured with it.
- Steps: [ ] tokens + CSS + engine  [ ] remap scene colours
  [ ] recapture gruvbox-light  [ ] stills of every scene checked  [ ] render

## Plan (1920x1080, 30 fps)

| # | id | seconds | content | status |
|---|----|---------|---------|--------|
| 1 | intro | 6.5 | types `argo-tui`, deletes it, types `micko`; wordmark, tagline | rendered |
| 2 | release | 4.5 | big "0.6" reveal, new name + mascot pills | rendered |
| 3 | bird | 8 | photos of Mićko, "named after an eastern rosella" | rendered |
| 4 | mascot | 7 | the ASCII Mićko perched on the real pane, across skins | rendered |
| 5 | list | 7 | real workflow list, phase glyphs, progress | rendered |
| 6 | timeline | 8 | Gantt timeline, critical path ◆, now line | rendered |
| 7 | explain | 7.5 | Explain cards: why a run failed, offline | rendered |
| 8 | nodes | 6.5 | pipeline tree, folds, info panel | rendered |
| 9 | logs | 6.5 | per-step labels, level colours, events | rendered |
| 10 | palette | 6.5 | `:` command palette typing, cron list | rendered |
| 11 | filter | 5.5 | query language `phase=Failed age<3h` | rendered |
| 12 | skins | 7 | wall of 13 skins | rendered |
| 13 | actions | 6.5 | marks + bulk actions, 4 outcomes | rendered |
| 14 | outro | 7 | wordmark, install, feature ticker | rendered |

## Notes / decisions

- Terminal screens are real captures of `./dist/micko --demo --skin tokyo-night`
  at 124x32 (the list shows its PROGRESS column from 120 pane columns), so the
  video shows what the binary actually draws. The mascot needs 40 rows, so the
  `mascot_*` shots are 124x40.
- Frames and per-scene mp4s live in `out/` (gitignored); only the final video is committed.
- The 0.5 build of this video is tagged by commit f93e21d-era history
  (`git log -- video/argo-tui-0.5.mp4`).

## How to work on a scene

- Preview in a browser: open `src/index.html` (scrubber + scene picker), or
  `src/index.html?scene=timeline&t=3` for one frame.
- Headless stills: `node render.mjs --still timeline:3 timeline:5.5` -> `out/stills/`.
- Contact sheet (every 0.5 s): `node render.mjs --sheet timeline`.
- A scene file is `src/scenes/NN-id.js`: `E.scene({id, dur, build(root) { ...; return (t) => {...} }})`.
  `update(t)` must derive everything from `t` (frames render out of order, in parallel).
- Terminal helpers: `E.term(parent, screen)` -> `{win, cell(r,c,r2,c2), show, mix}`;
  `E.cam(win, {fx, fy, X, Y, s})` puts window point (fx,fy) at stage (X,Y) with scale s;
  `E.hl(term, rect)` highlight box; `E.spot(term, rect)` dims the rest.
- Status values in the table: todo -> written (stills checked) -> rendered.
- A full render takes ~17-20 min with `--jobs 4` (~1.5 s a frame); re-render one
  scene with `node render.mjs <id>` (join + final encode run once all exist).
- If the demo data or UI changes: `npm run capture` (FORCE=1 to re-record all);
  `node capture/find.mjs <screen> "text"` prints where text sits.
- Soundtrack is synthesized (`node audio/soundtrack.mjs`); rerun it whenever
  scene durations change, since cuts and clicks are timed from them.

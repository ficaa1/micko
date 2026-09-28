# Motion graphic: micko 0.6 — progress log

This file is the hand-off note. If a session is cut off, a new one should read
this file first, then continue from the first unchecked item. Every step is
committed on its own, so `git log -- video/` shows where things stopped.

The project was argo-tui until 0.6. The 0.5 video (`argo-tui-0.5.mp4`) is
kept as it was released; the source now builds the 0.6 video, `micko-0.6.mp4`.
0.6 renames the tool to micko after Mićko, the owner's eastern rosella, and
adds him as an opt-in mascot (`--mascot`) that perches on the pane at 80x40+.

## Resume in one command

```sh
cd video && npm install && npm run all   # capture (if screens missing) + render + encode
```

`npm run render` skips any scene whose `out/scenes/<id>.mp4` is newer than its
source and the shared engine, so a re-run only redoes what changed.

## v0.6 draft: steps

- [ ] 1. Merge `main` (the rename) into this branch; `make build` gives `dist/micko`
- [ ] 2. Photos of Mićko into `src/photos/` (resized, EXIF stripped)
- [ ] 3. Recapture every screen with `dist/micko` (`FORCE=1 npm run capture`);
      add 40-row `--mascot` shots; check callout coordinates still line up
- [ ] 4. Rename in scenes: intro retypes `argo-tui` as `micko`, release rolls to 0.6,
      window titles, outro commands
- [ ] 5. New scenes: `bird` (photos) and `mascot` (the ASCII Mićko perched, across skins)
- [ ] 6. Soundtrack re-timed; full render -> `micko-0.6.mp4` + `poster.png`

## Plan (1920x1080, 30 fps)

| # | id | seconds | content | status |
|---|----|---------|---------|--------|
| 1 | intro | 6.5 | types `argo-tui`, deletes it, types `micko`; wordmark, tagline | todo |
| 2 | release | 4.5 | big "0.6" reveal, new name + mascot pills | todo |
| 3 | bird | 8 | photos of Mićko, "named after an eastern rosella" | todo |
| 4 | mascot | 7 | the ASCII Mićko perched on the real pane, across skins | todo |
| 5 | list | 7 | real workflow list, phase glyphs, progress | todo |
| 6 | timeline | 8 | Gantt timeline, critical path ◆, now line | todo |
| 7 | explain | 7.5 | Explain cards: why a run failed, offline | todo |
| 8 | nodes | 6.5 | pipeline tree, folds, info panel | todo |
| 9 | logs | 6.5 | per-step labels, level colours, events | todo |
| 10 | palette | 6.5 | `:` command palette typing, cron list | todo |
| 11 | filter | 5.5 | query language `phase=Failed age<3h` | todo |
| 12 | skins | 7 | wall of 13 skins | todo |
| 13 | actions | 6.5 | marks + bulk actions, 4 outcomes | todo |
| 14 | outro | 7 | wordmark, install, feature ticker | todo |

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

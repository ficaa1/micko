# Motion graphic: argo-tui 0.5 — progress log

This file is the hand-off note. If a session is cut off, a new one should read
this file first, then continue from the first unchecked item. Every step is
committed on its own, so `git log -- video/` shows where things stopped.

## Resume in one command

```sh
cd video && npm install && npm run all   # capture (if screens missing) + render + encode
```

`npm run render` skips any scene whose `out/scenes/<id>.mp4` is newer than its
source and the shared engine, so a re-run only redoes what changed.

## Plan (1920x1080, 30 fps, 78.5 s)

| # | id | seconds | content | status |
|---|----|---------|---------|--------|
| 1 | intro | 6 | typed `argo-tui --demo`, logo build, tagline | rendered |
| 2 | release | 4.5 | big "0.5" reveal, "what's new" | rendered |
| 3 | list | 7 | real workflow list, phase glyphs, progress | rendered |
| 4 | timeline | 8 | Gantt timeline, critical path ◆, now line | rendered |
| 5 | explain | 7.5 | Explain cards: why a run failed, offline | rendered |
| 6 | nodes | 6.5 | pipeline tree, folds, info panel | rendered |
| 7 | logs | 6.5 | per-step labels, level colours, events | rendered |
| 8 | palette | 6.5 | `:` command palette typing, cron list | rendered |
| 9 | filter | 5.5 | query language `phase=Failed age<3h` | rendered |
| 10 | skins | 7 | wall of 14 skins | rendered |
| 11 | actions | 6.5 | marks + bulk actions, 4 outcomes | rendered |
| 12 | outro | 7 | feature ticker, install, v0.5.0 | rendered |

## Steps

- [x] 1. Skeleton + this log committed
- [x] 2. `capture/capture.sh`: drive `argo-tui --demo` in tmux, save ANSI screens to `screens/*.ans`
- [x] 3. `capture/ansi2json.mjs`: ANSI -> `screens/*.json` (runs of text + colours)
- [x] 4. Engine: `src/index.html`, `src/engine.js` (deterministic `renderAt(sceneId, t)`)
- [x] 5. `render.mjs`: Playwright frames piped to ffmpeg per scene; `concat` to final mp4
- [x] 6. Scenes 1..12 (tick the table above as each renders cleanly)
- [x] 7. Soundtrack: `node audio/soundtrack.mjs` (synthesized, timed from scene durations; render.mjs muxes it)
- [x] 8. Final `argo-tui-0.5.mp4` (78.5 s, 1080p30, AAC, ~19 MB) + `poster.png` committed

## Notes / decisions

- Terminal screens are real captures of `./dist/argo-tui --demo --skin tokyo-night`
  at 124x32 (the list shows its PROGRESS column from 120 pane columns), so the video shows what the binary actually draws.
- Frames and per-scene mp4s live in `out/` (gitignored); only the final video is committed.

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

## Status

Complete: every scene is rendered and `argo-tui-0.5.mp4` is committed.
A full render takes ~17 min with `--jobs 4` (~1.5 s a frame), so when
changing one scene, re-render only it: `node render.mjs timeline` (the join
and final encode run automatically once all 12 scene files exist).

Ideas if picking this up again:
- A shorter cut (e.g. 30 s: intro, release, timeline, explain, skins, outro)
  by commenting scenes out of `src/index.html` and rerunning.
- A GIF/WebM teaser for the README from `out/master.mp4`.
- If the demo data or UI changes: `npm run capture` (FORCE=1 to re-record all),
  then `npm run render`. Callout coordinates live in each scene file;
  `node capture/find.mjs <screen> "text"` prints where text sits.
- Soundtrack is synthesized (`node audio/soundtrack.mjs`); rerun it whenever
  scene durations change, since cuts and clicks are timed from them.

# micko motion graphics: progress log

Hand-off note. If a session is cut off, read this first, then continue from the
first unchecked item. Every step is committed on its own (`git log -- video/`).
Commits are authored as ficaa1 with no attribution lines (see /CLAUDE.md).

## Layout

```
video/
  lib/                 shared toolkit (one copy for every video)
    engine.js          deterministic scene engine: E.scene, E.term, E.cam, E.hl, E.pill, ...
    style.css          "Mićko on paper" look: palette tokens, paper grain, TUI type
    player.js          browser preview (scrubber) for a video's index.html
    fonts/             JetBrains Mono (full), Inter, Space Grotesk
    capture.sh         records real micko screens in tmux (shot lists live per video)
    ansi2json.mjs      screens/*.ans -> screens.js for one video
    find.mjs           row/col of text in a captured screen (for callouts)
    render.mjs         Playwright frames -> ffmpeg, per scene, resumable; joins + final encode
    soundtrack.mjs     synthesized placeholder soundtrack timed from the scenes
  videos/<name>/       one folder per video
    index.html         the scene list, in order (the video's "composition")
    scenes/NN-id.js    one scene per file
    shots.sh           which screens to record, and with which skin/binary
    screens/*.ans      recorded screens (committed), screens.js generated from them
  assets/photos/       photos of Mićko (resized, EXIF stripped)
  renders/             finished videos and review sheets (committed)
  out/                 scratch: per-scene mp4s, stills, soundtracks (gitignored)
```

Commands (from `video/`):

```sh
npm install                                   # once; plus `npx playwright install chromium` off this container
bash lib/capture.sh tour                      # record screens missing from videos/tour/screens (FORCE=1 = all)
node lib/render.mjs tour --still list:3       # stills -> out/tour/stills/
node lib/render.mjs tour --sheet list         # contact sheet of one scene
node lib/render.mjs tour --jobs 8             # render stale scenes, join, encode renders/tour.mp4
node lib/render.mjs tour --music song.mp3     # your own track instead of the synthesized one
```

Open `videos/<name>/index.html` in a browser for a scrubbing preview.

## Videos

| video | what | status |
|---|---|---|
| `argo-tui-0.5.mp4` | the 0.5 release (dark, tokyo-night); its source is in git history | final |
| `release-0.6` | the 0.6 release: rename to micko, Mićko the mascot | drafts: `renders/micko-0.6-draft.mp4` (dark), `renders/micko-0.6-paper-draft.mp4` (paper + monokai, current look). Owner still to refine, add music, render on own PC. Screens recorded with the 0.6.0 binary: to re-record, build tag v0.6.0 first. |
| `tour` | general presentation of micko, not tied to a release | in progress, see below |

## Tour: plan

Owner's brief: same look as the 0.6 paper draft; not a release video but a
general presentation; first and foremost what the tool does; show Mićko's 0.7
animations when showing him, briefly.

| # | id | s | content | status |
|---|----|---|---------|--------|
| 1 | intro | 6 | types `micko --demo`; wordmark, tagline, phase chips | written |
| 2 | what | 6 | four jobs as cards: browse, understand, follow, act | written |
| 3 | list | 7 | the workflow list: phases, progress, messages | written |
| 4 | timeline | 8 | Gantt timeline, critical path, now line | written |
| 5 | explain | 7.5 | why a run failed, offline | written |
| 6 | nodes | 6.5 | pipeline tree, retries, info panel | written |
| 7 | logs | 6.5 | labelled logs, level colours, live events | written |
| 8 | palette | 6.5 | `:` command palette, cron list | written |
| 9 | filter | 5.5 | query language | written |
| 10 | actions | 6.5 | marks, bulk actions, four outcomes, read-only default | written |
| 11 | connect | 5.5 | profiles, managed kubectl port-forward, tokens, TLS | written |
| 12 | skins | 7 | wall of skins | written |
| 13 | micko | 6.5 | photo, then the 0.7 animations: perched (blink, look) and floor (kiss) | written |
| 14 | outro | 7 | wordmark + Mićko, install, feature ticker | written |

Steps:
- [x] 1. Merge main (0.7.0: Mićko animates on the perch and sits on the floor)
- [x] 2. Restructure into lib/ + videos/<name>/ (release-0.6 still renders the same)
- [x] 3. `anim` shots: record Mićko's loops at 10 fps, keep distinct poses + timing
- [x] 4. Tour scenes written, stills checked (`renders/tour-review.png`); 92.5 s
- [ ] 5. Full render -> `renders/micko-tour.mp4`
- [ ] 6. Skill: `.claude/skills/motion-graphics/` so the next video is quicker

## Style decisions (shared by release-0.6 and tour)

- Palette from Mićko's plumage, printed like riso ink on warm paper: crimson
  `#d7263d`, cobalt `#3f5bb8`, violet-blue `#6f7fd6`, yellow `#f2c14e` /
  `#e3a21a`, olive `#6f8a2a`, cream `#fbf8f1`; ink `#1f1a17` on paper `#efe4cf`.
- Paper background with grain that shifts 12 times a second, faint grid, soft
  mottling. No glows or dark gradients.
- JetBrains Mono throughout; kickers as box-drawing rules (`╭─ KICKER ──`),
  titles end in a blinking block cursor; the wordmark and big numbers are
  printed off-register (crimson with a cobalt plate).
- Cards, pills, keycaps and windows are paper cut-outs: ink border, hard
  offset shadow. Photos are taped on with washi tape.
- Terminal skin: monokai (owner's pick over gruvbox-light, which blended into
  the paper). Dark punchy window on cream.

## Lessons (also go into the skill)

- Render is CPU-bound (~1.5-3 s a frame on 4 cores); a 90 s video is ~30 min.
  Only stale scenes re-render; parallel `--jobs` ~ cores/2.
- Fonts: fontsource subsets split latin / latin-ext; "ć" needs latin-ext with
  unicode-range, or it falls back to another face.
- Block elements (█▌▐░) drawn as CSS cells, not glyphs, or bars show seams.
- A freshly built scene must wait for `img.decode()`, or the first frames
  show empty photos.
- Crossfading two terminal screens looks like garbage; wipe between them.
- Uploads to the user are capped at 30 MiB: send a lighter preview encode.
- Keep drafts under their own names; renders write a new file.

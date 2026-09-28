---
name: motion-graphics
description: Make motion graphic videos (release announcements, product tours, feature spotlights, launch trailers) for terminal apps, CLIs and developer tools, rendered to MP4 from real screen captures. Scenes are HTML animated deterministically and rendered frame by frame with Playwright and ffmpeg; the app's real screens are recorded in tmux; a placeholder soundtrack is synthesized. Use this whenever someone asks for a video, promo, trailer, demo reel, animated walkthrough, release video or "motion graphic" of a tool, TUI or CLI, wants to turn screenshots or a README into a video, or wants to change, restyle, re-render or add music to an existing one, even if they do not say "motion graphics".
---

# Motion graphics for terminal tools

You build the video as code: each scene is a small JavaScript file that draws
one moment on a 1920x1080 HTML stage as a function of time. A renderer
screenshots every frame in headless Chromium and ffmpeg encodes them. The app's
own screens are recorded for real in tmux, so the video shows what the product
actually draws, not a mock-up.

Everything needed is in this skill:
- `toolkit/`: the engine, theme, renderer, recorder, soundtrack (copied into the project as `lib/`)
- `scripts/scaffold.sh`: creates the project folder, or adds a video to it
- `templates/`: starter files (shot list, two example scenes, progress log)
- `references/engine-api.md`: every `E.*` helper, read before writing scenes
- `references/scene-patterns.md`: proven scene shapes, with where to copy them from
- `references/lessons.md`: pitfalls already paid for, read once per project

A complete worked example (two videos, 28 scenes) lives in the micko repo under
`video/`; its `PROGRESS.md` shows the process end to end.

## Workflow

Videos take long enough to render that sessions get interrupted. Keep state in
files: the brief, plan, decisions and a step checklist go in the project's
`PROGRESS.md`, and you commit after every step, so a new session can resume.

### 1. Brief

Settle before building: what the video is for (release, general tour, one
feature), who watches it and where, the length (60-95 s is typical for 12-14
scenes), and the look. For a release, lead with what changed; for a tour, lead
with what the tool does, and keep mascots and extras short. Ask when these are
unclear; they decide everything downstream.

The look matters and has a default trap: left alone, it drifts to dark
gradients, glowing blobs and blue-purple gradient text, which is what everyone
else's generated video looks like. Derive a palette from the subject (logo,
mascot, the product's own theme), choose a texture and type on purpose, and
record the reasons. The toolkit's `style.css` ships one such theme ("paper":
riso-ink colours on warm paper, monospace type, cut-out cards with hard
shadows); retheme it per project by changing its tokens and a few rules.

### 2. Set up

```sh
bash <skill>/scripts/scaffold.sh video NAME     # project in ./video, first video NAME
cd video && npm install && npx playwright install chromium
```

Commit `lib/`, the video folder, `PROGRESS.md`; `out/` is scratch (gitignored).
Write the plan table in `PROGRESS.md`: one row per scene with id, seconds and
content.

### 3. Record real screens

Edit `videos/NAME/shots.sh`: `BIN` (the app, ideally in a demo/offline mode),
`ARGS`, the theme, then one line per screen you need:

```sh
shot list_0 --                          # a screen after no keys
shot detail -- Down Down Enter          # after keys (tmux names: Enter, Escape, Space, Down, C-c)
type_shot search -- / ++ "phase=Failed" # one screen per typed character
anim spinner -- ++ 42                   # an animation the app draws: distinct frames + timings
```

Then `bash lib/capture.sh NAME` (existing screens are kept; `FORCE=1` redoes
all). Add each theme's default colours to `skins.json` (the app never paints
the terminal's own background/foreground). `node lib/find.mjs NAME SCREEN
"text"` prints where text sits, for placing callouts. If the app has several
themes, render one still of the same screen in 5-6 of them on the real
background and let the owner choose before going further.

### 4. Write scenes

One file per scene in `videos/NAME/scenes/NN-id.js`, listed in order in
`videos/NAME/index.html`. Read `references/engine-api.md` first, then pick
shapes from `references/scene-patterns.md`. Principles that make the result
read well:
- One idea per scene, a 1-3 line headline that states it, and 2-3 callouts at
  most, each on screen long enough to read (about 1.2 s).
- Show the real screen, then point at the part that matters (highlight + dim +
  pill). Draw designed cards only for what the demo cannot show, and only
  claim what the docs say.
- Motion has a purpose: things enter, one thing is emphasised, things leave.
  Switch screens instantly or with a wipe, never a crossfade of two terminals.
- `update(t)` derives everything from `t`, because frames render out of order.
- Compute positions from the captured data where you can, so a re-recording
  does not break them.
- Add `sfx` cues (clicks under typing, a thump when something lands).

### 5. Review stills before rendering

```sh
node lib/render.mjs NAME --still intro:2.5 list:4      # single frames
node lib/render.mjs NAME --sheet list                  # a frame every 0.5 s
```

Look at every scene's key frame and fix overflow, overlaps and crops before
spending render time. A tiled sheet of one frame per scene (ffmpeg `tile`) is a
good thing to show the owner. Browser preview: open `videos/NAME/index.html`.

### 6. Render and deliver

```sh
node lib/render.mjs NAME --jobs 4      # ~cores/2; only stale scenes re-render; resumable
```

It joins the scenes with the soundtrack and encodes the file named by
`<meta name="output">` in index.html (default `renders/NAME.mp4`). Expect
~1.5-3 s per frame per job; run it in the background and check the result with
a frame sheet of the whole video (`ffmpeg -vf fps=1/3,scale=384:-1,tile=6x6`).
Keep each draft under its own name, and if the file is too big to send (30
MiB here), send a lighter preview encode and keep the full one in the repo.

Music: the synthesized soundtrack is a placeholder. `--music FILE` or
`videos/NAME/music.mp3` replaces it (trimmed, 2 s fade-out); give the owner the
scene start times so they can sync.

## Adding a video to an existing project

`bash <skill>/scripts/scaffold.sh video NEW` adds `videos/NEW` and leaves the
rest alone. Each video has its own shot list and screens, so re-recording one
(say, with a newer build) never changes another that is still being refined.
Copy scenes between videos freely; they only depend on `lib/` and their own
screens.

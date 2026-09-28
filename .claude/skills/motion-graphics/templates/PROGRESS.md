# Motion graphics: progress log

Hand-off note. If a session is cut off, the next one reads this first and
continues from the first unchecked step. Commit after every step.

## Brief

- What the video is for (release? tour? one feature?), who watches it, where it is posted:
- Length target:
- Look (palette source, texture, type, terminal theme) and why:
- Music: synthesized placeholder until the owner's track (`videos/NAME/music.mp3`)

## Plan (1920x1080, 30 fps)

| # | id | s | content | status |
|---|----|---|---------|--------|
| 1 | title | 4.5 | | todo |

Status: todo -> written (stills checked) -> rendered.

## Steps

- [ ] 1. Brief and plan agreed
- [ ] 2. Shot list; screens recorded (`bash lib/capture.sh NAME`)
- [ ] 3. Scenes written; stills and contact sheets checked
- [ ] 4. Full render -> `renders/NAME.mp4`; preview under 30 MiB sent
- [ ] 5. Owner's refinements and music

## Decisions

## Commands (from this folder)

```sh
npm install && npx playwright install chromium   # once
bash lib/capture.sh NAME                         # record missing screens (FORCE=1 = all)
node lib/render.mjs NAME --still title:2         # one frame -> out/NAME/stills/
node lib/render.mjs NAME --sheet title           # contact sheet, a frame every 0.5 s
node lib/render.mjs NAME --jobs 8                # render stale scenes, join, encode
```

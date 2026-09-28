# Engine API (lib/engine.js)

The page is a 1920x1080 stage. A scene is a plain object registered with
`E.scene`; `build(root)` creates its DOM once and returns `update(t)`, which
sets every animated property from `t` (seconds into the scene) alone. Frames are
rendered out of order and in parallel pages, so `update` must never depend on
the previous frame. Everything under `#scene` is `position: absolute`.

## Scenes

```js
E.scene({
  id: 'timeline', dur: 8,              // id matches the file name NN-timeline.js
  sfx: [[0.35, 'impact'], ...E.typing(1.0, 12, 0.08)],  // optional soundtrack cues
  build(root) {
    // create elements here
    return (t) => { /* set everything from t */ };
  },
});
```

- `sfx` cues: `'click'` (a key), `'thump'` (something lands), `'impact'` (a big
  reveal). `E.typing(start, n, per)` makes n clicks, one every `per` seconds.
- `E.ASSETS` is `'../../assets/'` (photos: `E.ASSETS + 'photos/x.jpg'`).
- `E.tok('red')` reads a theme token from style.css; in CSS values write `'var(--red)'`.

## Timing

- `E.p(t, a, b, ease='out')`: 0 before a, 1 after b, eased in between.
- `E.inOut(t, a, b, d=0.4)`: in over [a, a+d], out over [b, b+d] (0..1..0).
- Eases: `lin out in inOut expo back spring`.
- `E.lerp(a, b, p)`, `E.clamp(v, lo=0, hi=1)`.

## DOM

- `E.el(tag, cls, parent, html)`: create and append.
- `E.css(n, {left: 10, fontSize: 24, lineHeight: 1.4})`: numbers get `px` except
  opacity, zIndex, lineHeight, fontWeight, flex.
- `E.set(n, {x, y, s, sx, sy, r, rx, ry, persp, o, blur})`: transform + opacity +
  blur in one call. Only sets `transform` when a transform key is given.
- `E.words(parent, text)` splits text (with `\n` for line breaks) into word
  spans; `E.revealWords(spans, t, start, gap=0.06, d=0.5)` reveals them in turn.
- `E.headline(root, {x, y, width, kicker, title, body, align})` returns
  `{box, update(t, in, out)}`: kicker as a box-drawing rule, title with a
  blinking block cursor, body copy; words reveal in, the block fades out.
- `E.pill(parent, html, color)`: a callout with readable text on any colour.
- `E.pop(n, t, in, out=99, extra={})`: spring pop-in / fade-out for small things.

## Terminals (real captured screens)

```js
const term = E.term(root, 'list_0', { title: 'app --demo', rows: 32 });
term.layer('list_1');                 // pre-build other screens in the same window
term.show('list_1');                  // switch instantly (like the app redrawing)
term.wipe('a', 'b', p);               // left-to-right wipe between two screens (0..1)
const r = term.cell(row, col, row2, col2);   // {x, y, w, h} in window coordinates
```

- The window: title bar + 124x32 cells by default (`E.CW` 9 px, `E.LH` 20 px).
  `rows` crops the window to the top N rows.
- `E.cam(term.win, {fx, fy, X, Y, s, rx, ry, persp, o})`: puts window point
  (fx, fy) at stage point (X, Y) at scale s. Interpolate plain numbers between
  camera states (`E.mixCam(a, b, p)`, `E.camPath(t, [[time, cam], ...])`).
- `E.toStage(cam, wx, wy)`: stage position of a window point (for pills).
- `E.hl(term, rect, color)`: highlight box; animate with `E.set(box, {o})`.
- `E.spot(term, rect)` -> `{place(rect), set(o)}`: dims everything else.
- `window.SCREENS[name].rows`: runs `[text, fg, bg, flags]` per row; use it to
  compute positions from content (see scene-patterns.md "find it in the data").
- `window.SEQS[name]`: `[[seconds, frame], ...]` from an `anim` recording.

## Rendering hooks

`E.renderAt(id, t)` builds the scene if needed (waiting for its images to
decode) and draws frame t; render.mjs and the preview player both call it.

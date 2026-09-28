// Deterministic animation engine. A scene is { id, dur, build(root, api) }:
// build creates the DOM once and returns update(t), which must set every
// animated property from t (seconds into the scene) alone, so any frame can
// be rendered in any order. render.mjs and player.js both call E.renderAt.
(function () {
  const E = (window.E = {});
  E.W = 1920; E.H = 1080; E.FPS = 30;
  E.scenes = [];
  E.scene = (s) => E.scenes.push(s);

  // ---- maths -------------------------------------------------------------
  E.clamp = (v, a = 0, b = 1) => Math.min(b, Math.max(a, v));
  E.lerp = (a, b, p) => a + (b - a) * p;
  E.ease = {
    lin: (x) => x,
    out: (x) => 1 - Math.pow(1 - x, 3),
    in: (x) => x * x * x,
    inOut: (x) => (x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2),
    expo: (x) => (x >= 1 ? 1 : 1 - Math.pow(2, -10 * x)),
    back: (x) => { const c1 = 1.70158, c3 = c1 + 1; return 1 + c3 * Math.pow(x - 1, 3) + c1 * Math.pow(x - 1, 2); },
    spring: (x) => (x >= 1 ? 1 : 1 - Math.exp(-6 * x) * Math.cos(11 * x)),
  };
  // Progress of t through [a, b], eased; 0 before a, 1 after b.
  E.p = (t, a, b, ease = 'out') => E.ease[ease](E.clamp((t - a) / (b - a)));
  // In at [a, a+d], out at [b, b+d]: 0..1..0.
  E.inOut = (t, a, b, d = 0.4, ease = 'out') => Math.min(E.p(t, a, a + d, ease), 1 - E.p(t, b, b + d, 'in'));

  // ---- DOM ---------------------------------------------------------------
  E.el = (tag, cls, parent, html) => {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (html !== undefined) n.innerHTML = html;
    if (parent) parent.appendChild(n);
    return n;
  };
  E.css = (n, o) => { for (const k in o) n.style[k] = typeof o[k] === 'number' && !/opacity|zIndex/.test(k) ? o[k] + 'px' : o[k]; return n; };
  // Transform + opacity in one call: {x, y, s, sx, sy, r, rx, ry, o, blur}.
  E.set = (n, v) => {
    const tr = [];
    if (v.x !== undefined || v.y !== undefined) tr.push(`translate(${v.x || 0}px,${v.y || 0}px)`);
    if (v.persp) tr.unshift(`perspective(${v.persp}px)`);
    if (v.rx) tr.push(`rotateX(${v.rx}deg)`);
    if (v.ry) tr.push(`rotateY(${v.ry}deg)`);
    if (v.r) tr.push(`rotate(${v.r}deg)`);
    if (v.s !== undefined) tr.push(`scale(${v.s})`);
    if (v.sx !== undefined || v.sy !== undefined) tr.push(`scale(${v.sx ?? 1},${v.sy ?? 1})`);
    if (tr.length) n.style.transform = tr.join(' ');
    if (v.o !== undefined) { n.style.opacity = v.o; n.style.visibility = v.o <= 0.001 ? 'hidden' : 'visible'; }
    if (v.blur !== undefined) n.style.filter = v.blur > 0.05 ? `blur(${v.blur}px)` : 'none';
    return n;
  };
  E.esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

  // Split text into word spans for staggered reveals. Returns the spans.
  E.words = (parent, text, cls = '') => {
    parent.innerHTML = '';
    const out = [];
    text.split('\n').forEach((line, i) => {
      if (i) E.el('br', '', parent).style.position = 'static';
      line.split(/( +)/).filter(Boolean).forEach((w) => out.push(E.el('span', 'w ' + cls, parent, E.esc(w))));
    });
    return out;
  };
  // Reveal word spans from time a, one every `gap` seconds.
  E.revealWords = (spans, t, a, gap = 0.06, d = 0.5) => spans.forEach((s, i) => {
    const p = E.p(t, a + i * gap, a + i * gap + d);
    E.set(s, { y: (1 - p) * 28, o: p, blur: (1 - p) * 8 });
  });

  // Headline block: kicker, title, body, positioned at (x, y).
  E.headline = (root, { x, y, kicker, title, body, width = 720, align = 'left' }) => {
    const box = E.el('div', '', root); E.css(box, { left: x, top: y, width });
    const k = E.el('div', 'kicker', box); k.textContent = kicker || '';
    const ti = E.el('div', 'title', box); E.css(ti, { top: 40, textAlign: align, width });
    const titleSpans = E.words(ti, title);
    const b = E.el('div', 'body', box); E.css(b, { top: 40 + 102 * (title.split('\n').length) + 14, width, whiteSpace: 'normal', textAlign: align });
    const bodySpans = E.words(b, body || '');
    ti.style.whiteSpace = 'pre';
    return {
      box,
      update(t, a = 0, out = 99) {
        const po = 1 - E.p(t, out, out + 0.45, 'in');
        const pk = E.p(t, a, a + 0.5);
        E.set(k, { x: (1 - pk) * -30, o: pk * po });
        E.revealWords(titleSpans, t, a + 0.1, 0.07, 0.6);
        E.revealWords(bodySpans, t, a + 0.45, 0.025, 0.5);
        E.set(box, { o: po, y: (1 - po) * -20 });
      },
    };
  };

  // ---- terminal ----------------------------------------------------------
  E.CW = 9; E.LH = 20; E.BAR = 38; E.PAD = 14;
  const htmlCache = {};
  // Block elements drawn as cell backgrounds, so bars are gapless at any scale.
  const lg = (dir, a, b) => `linear-gradient(${dir},${a},${b})`;
  const cut = (dir, pct) => `linear-gradient(${dir},currentColor ${pct}%,transparent ${pct}%)`;
  const BLOCKS = { '█': 'currentColor', '▀': cut('180deg', 50), '▄': cut('0deg', 50), '▐': cut('270deg', 50), '▕': cut('270deg', 12.5), '▔': cut('180deg', 12.5), '▁': cut('0deg', 12.5) };
  ['▏', '▎', '▍', '▌', '▋', '▊', '▉'].forEach((ch, i) => (BLOCKS[ch] = cut('90deg', (i + 1) * 12.5)));
  BLOCKS['░'] = lg('0deg', 'color-mix(in srgb,currentColor 28%,transparent)', 'color-mix(in srgb,currentColor 28%,transparent)');
  BLOCKS['▒'] = lg('0deg', 'color-mix(in srgb,currentColor 50%,transparent)', 'color-mix(in srgb,currentColor 50%,transparent)');
  BLOCKS['▓'] = lg('0deg', 'color-mix(in srgb,currentColor 75%,transparent)', 'color-mix(in srgb,currentColor 75%,transparent)');
  E.screenHTML = (name) => {
    if (htmlCache[name]) return htmlCache[name];
    const sc = window.SCREENS[name];
    if (!sc) throw new Error('no screen ' + name);
    const sk = window.SKINS[sc.skin];
    let h = '';
    for (const row of sc.rows) {
      h += '<div class="r">';
      for (const [text, fg, bg, fl] of row) {
        let st = `color:${fg || sk.text};`;
        if (bg) st += `background:${bg};`;
        if (fl & 1) st += 'font-weight:700;';
        if (fl & 2) st += 'opacity:.6;';
        if (fl & 8) st += 'text-decoration:underline;';
        let inner = '';
        for (const ch of text) {
          const c = ch.codePointAt(0);
          if (c < 128) inner += E.esc(ch);
          else if (BLOCKS[ch]) inner += `<i class="${'░▒▓'.includes(ch) ? 'k s' : 'k'}" style="background:${BLOCKS[ch]}"></i>`;
          else if (c >= 0x2500 && c <= 0x257f) inner += ch; // box drawing: in the font
          else inner += `<i>${ch}</i>`;
        }
        h += `<span${bg ? ' class="b"' : ''} style="${st}">${inner}</span>`;
      }
      h += '</div>';
    }
    return (htmlCache[name] = h);
  };
  E.COLS = 124; E.ROWS = 32;
  // A terminal window showing screen `name`. Its origin is the window's top
  // left; cell(row, col) gives a cell's position inside it.
  E.term = (parent, name, { title = 'micko --demo', cols = E.COLS, rows = E.ROWS } = {}) => {
    const sk = window.SKINS[window.SCREENS[name].skin];
    const w = cols * E.CW + E.PAD * 2, h = rows * E.LH + E.BAR + E.PAD * 2;
    const win = E.el('div', 'win', parent); E.css(win, { left: 0, top: 0, width: w, height: h, background: sk.bg });
    const bar = E.el('div', 'bar', win);
    ['#ff5f57', '#febc2e', '#28c840'].forEach((c, i) => E.css(E.el('div', 'dot', bar), { left: 16 + i * 20, background: c }));
    const ttl = E.el('div', 'ttl', bar); ttl.textContent = title;
    const body = E.el('div', 'term', win); E.css(body, { left: E.PAD, top: E.BAR + E.PAD, width: cols * E.CW, height: rows * E.LH, overflow: 'hidden' });
    const layers = {};
    const api = {
      win, body, w, h, skin: sk, current: null,
      layer(n) { // one child per screen so switching is an opacity change
        if (!layers[n]) {
          // Each layer paints its own screen's skin, so one window can cycle skins.
          const sc = window.SCREENS[n], d = E.el('div', '', body);
          E.css(d, { left: 0, top: 0, width: cols * E.CW, height: sc.rows.length * E.LH, background: window.SKINS[sc.skin].bg });
          d.innerHTML = E.screenHTML(n); layers[n] = d;
        }
        return layers[n];
      },
      show(n, o = 1) {
        api.layer(n); for (const k in layers) E.set(layers[k], { o: k === n ? o : 0 }); api.current = n;
        win.style.background = window.SKINS[window.SCREENS[n].skin].bg;
      },
      // Crossfade from a to b by p.
      mix(a, b, p) { api.layer(a); api.layer(b); for (const k in layers) E.set(layers[k], { o: k === a ? 1 : k === b ? p : 0 }); layers[b].style.zIndex = 2; layers[a].style.zIndex = 1; },
      // Wipe from a to b by p, left to right (b is revealed behind a moving edge).
      wipe(a, b, p) {
        api.layer(a); api.layer(b);
        for (const k in layers) { E.set(layers[k], { o: k === a || k === b ? 1 : 0 }); layers[k].style.clipPath = ''; }
        layers[a].style.zIndex = 1; layers[b].style.zIndex = 2;
        layers[b].style.clipPath = `inset(0 ${(1 - p) * 100}% 0 0)`;
        if (p <= 0) E.set(layers[b], { o: 0 });
        api.current = p >= 1 ? b : a;
      },
      // Cell rectangle in window coordinates.
      cell(row, col, row2 = row, col2 = col) {
        return { x: E.PAD + col * E.CW, y: E.BAR + E.PAD + row * E.LH, w: (col2 - col + 1) * E.CW, h: (row2 - row + 1) * E.LH };
      },
    };
    api.show(name);
    return api;
  };
  // Camera over a window: puts window point (fx, fy) at stage point (X, Y)
  // with scale s. Interpolate the plain numbers and call again.
  E.cam = (n, { fx, fy, X = E.W / 2, Y = E.H / 2, s = 1, o, ry = 0, rx = 0, persp = 0 }) => {
    n.style.transformOrigin = `${fx}px ${fy}px`;
    const tr = `translate(${X - fx}px,${Y - fy}px)` + (persp ? ` perspective(${persp}px)` : '') + (ry ? ` rotateY(${ry}deg)` : '') + (rx ? ` rotateX(${rx}deg)` : '') + ` scale(${s})`;
    n.style.transform = tr;
    if (o !== undefined) { n.style.opacity = o; n.style.visibility = o <= 0.001 ? 'hidden' : 'visible'; }
  };
  // Stage position of window point (wx, wy) under camera c (no rotation).
  E.toStage = (c, wx, wy) => ({ x: (c.X ?? E.W / 2) + (wx - c.fx) * (c.s ?? 1), y: (c.Y ?? E.H / 2) + (wy - c.fy) * (c.s ?? 1) });
  E.mixCam = (a, b, p) => { const o = {}; for (const k of new Set([...Object.keys(a), ...Object.keys(b)])) o[k] = E.lerp(a[k] ?? b[k], b[k] ?? a[k], p); return o; };

  // Keyframed camera: keys = [[time, cam], ...]; eased moves between them.
  E.camPath = (t, keys, ease = 'inOut') => {
    if (t <= keys[0][0]) return { ...keys[0][1] };
    for (let i = 1; i < keys.length; i++) {
      if (t <= keys[i][0]) return E.mixCam(keys[i - 1][1], keys[i][1], E.ease[ease]((t - keys[i - 1][0]) / (keys[i][0] - keys[i - 1][0])));
    }
    return { ...keys[keys.length - 1][1] };
  };
  // Highlight box around a rect in a window's coordinates (added to the window).
  E.hl = (term, rect, color = '#7dcfff') => {
    const n = E.el('div', 'hl', term.win);
    E.css(n, { left: rect.x - 5, top: rect.y - 3, width: rect.w + 10, height: rect.h + 6, borderColor: color, zIndex: 5,
      boxShadow: `0 0 0 4px ${color}24, 0 0 30px ${color}66` });
    return n;
  };
  // Dim everything in a window except rect: four panes.
  E.spot = (term, rect) => {
    const ns = [0, 1, 2, 3].map(() => { const d = E.el('div', 'dim-mask', term.win); d.style.zIndex = 4; return d; });
    const { w, h } = term;
    const place = (r) => {
      E.css(ns[0], { left: 0, top: 0, width: w, height: Math.max(0, r.y - 4) });
      E.css(ns[1], { left: 0, top: r.y + r.h + 4, width: w, height: Math.max(0, h - r.y - r.h - 4) });
      E.css(ns[2], { left: 0, top: r.y - 4, width: Math.max(0, r.x - 6), height: r.h + 8 });
      E.css(ns[3], { left: r.x + r.w + 6, top: r.y - 4, width: Math.max(0, w - r.x - r.w - 6), height: r.h + 8 });
    };
    place(rect);
    return { place, set: (o) => ns.forEach((d) => E.set(d, { o })) };
  };
  E.pill = (parent, text, color = '#7dcfff') => { const n = E.el('div', 'pill', parent); n.innerHTML = text; n.style.background = color; return n; };

  // Pop-in helper for small elements: scale from .6 with a spring.
  E.pop = (n, t, a, out = 99, extra = {}) => {
    const p = E.p(t, a, a + 0.55, 'back'), q = 1 - E.p(t, out, out + 0.35, 'in');
    E.set(n, { ...extra, s: (0.6 + 0.4 * p) * (0.9 + 0.1 * q), o: E.clamp(p * 1.6) * q, y: (extra.y || 0) + (1 - p) * 16 });
  };

  // ---- timeline ----------------------------------------------------------
  E.total = () => E.scenes.reduce((a, s) => a + s.dur, 0);
  E.offset = (id) => { let o = 0; for (const s of E.scenes) { if (s.id === id) return o; o += s.dur; } return 0; };
  E.byId = (id) => E.scenes.find((s) => s.id === id);
  E.at = (T) => { let o = 0; for (const s of E.scenes) { if (T < o + s.dur) return { s, t: T - o }; o += s.dur; } const s = E.scenes[E.scenes.length - 1]; return { s, t: s.dur - 1e-6 }; };

  function background(T) {
    const o = document.querySelectorAll('#bg .orb');
    E.set(o[0], { x: 200 + Math.sin(T * 0.21) * 260, y: -200 + Math.cos(T * 0.17) * 140 });
    E.set(o[1], { x: 1150 + Math.cos(T * 0.19) * 240, y: 380 + Math.sin(T * 0.23) * 160 });
    E.set(o[2], { x: 600 + Math.sin(T * 0.13 + 2) * 420, y: 620 + Math.cos(T * 0.29) * 120 });
    document.querySelector('#bg .grid').style.backgroundPosition = `0 ${(T * 40) % 80}px`;
  }

  let live = null; // { id, update }
  E.renderAt = (id, t) => {
    const s = E.byId(id);
    if (!live || live.id !== id) {
      const root = document.getElementById('scene');
      root.innerHTML = ''; root.removeAttribute('style');
      live = { id, update: s.build(root) };
    }
    background(E.offset(id) + t);
    live.update(t);
  };
  E.renderGlobal = (T) => { const { s, t } = E.at(T); E.renderAt(s.id, t); };
  E.ready = () => document.fonts.ready;
})();

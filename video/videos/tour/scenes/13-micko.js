// Mićko, briefly: the photo he is drawn from, and his real 0.7 routines in
// two close-ups, perched on the pane and on its floor by his mirror. Poses
// are frames recorded from the app (`anim` shots); the order is his own
// routine, played faster than life (a real pass takes about 45 s).
E.scene({
  id: 'micko', dur: 6.5,
  sfx: [[0.55, 'thump']],
  build(root) {
    const h = E.headline(root, { x: 110, y: 90, width: 600, kicker: 'the mascot', title: 'And Mićko', body: 'An eastern rosella, drawn in ASCII. Off by default.' });

    const photo = E.el('div', '', root);
    E.css(photo, { left: 130, top: 470, width: 470, height: 352, borderRadius: 6, background: '#fbf8f1', border: '10px solid #fbf8f1', boxShadow: '0 0 0 2.5px #1f1a17, 9px 9px 0 2.5px #1f1a17' });
    const img = E.el('img', '', photo); img.src = E.ASSETS + 'photos/micko-monitor.jpg';
    E.css(img, { left: 0, top: 0, width: '100%', height: '100%', objectFit: 'cover', objectPosition: '65% 55%' });
    E.css(E.el('div', 'tape', photo), { top: -18, left: 170, background: '#f2c14e', transform: 'rotate(-4deg)', zIndex: 2 });

    // Bounding box of what is drawn in rows r0..r1 (cols c0..c1) across frames.
    const rowsOf = (n) => window.SCREENS[n].rows.map((r) => [...r.map((x) => x[0]).join('')]);
    const bbox = (frames, r0, r1, c0, c1) => {
      let a = 999, b = 0;
      for (const f of frames) { const rows = rowsOf(f); for (let r = r0; r <= r1; r++) rows[r].forEach((ch, c) => { if (c >= c0 && c <= c1 && ch.trim() && ch !== '│') { a = Math.min(a, c); b = Math.max(b, c); } }); }
      return [a, b];
    };
    const frames = (p) => Object.keys(window.SCREENS).filter((k) => k.startsWith(p + '_')).sort();
    const perchF = frames('perch'), floorF = frames('floor');
    const border0 = rowsOf('perch_000').findIndex((r) => r.includes('╭'));
    const floorRows = rowsOf('floor_000'), bottom = floorRows.findIndex((r) => r[0] === '╰');

    // A close-up: a paper frame that crops a whole terminal to one region.
    const lens = (first, all, r0, r1, c0, c1, S, x, y, label) => {
      const box = E.el('div', '', root);
      const term = E.term(box, first, { rows: 40, title: '' });
      all.forEach((f) => term.layer(f));
      const reg = term.cell(r0, c0, r1, c1), M = 26, MV = 3;
      const w = (reg.w + M * 2) * S, hh = (reg.h + MV * 2) * S;
      E.css(box, { left: x, top: y, width: w, height: hh, overflow: 'hidden', borderRadius: 10, border: '3px solid #1f1a17', boxShadow: '10px 10px 0 #1f1a17', background: term.skin.bg });
      E.cam(term.win, { fx: reg.x + reg.w / 2, fy: reg.y + reg.h / 2, X: w / 2 - 3, Y: hh / 2 - 3, s: S });
      term.win.style.boxShadow = 'none'; term.win.style.border = 'none';
      const chip = E.el('div', 'chip', root, label); E.css(chip, { left: x, top: y + hh + 22, fontSize: 22 });
      return { box, term, chip };
    };
    const [pc0, pc1] = bbox(perchF, 1, border0 - 1, 0, 123);
    const perch = lens('perch_000', perchF, 1, border0, pc0 - 2, pc1 + 2, 2.1, 760, 150, '<span style="color:#d7263d">--mascot</span>  perched on the pane');
    const [fc0, fc1] = bbox(floorF, bottom - 3, bottom - 1, 60, 123);
    const floor = lens('floor_000', floorF, bottom - 3, bottom, fc0 - 2, fc1 + 1, 2.1, 1080, 560, '<span style="color:#d7263d">--mascot=floor</span>  by his mirror');

    // His routines, faster than life. [seconds from T0, frame]
    const T0 = 1.0;
    const perchSeq = [[0, 'perch_000'], [0.9, 'perch_001'], [1.05, 'perch_000'], [1.6, 'perch_002'], [2.3, 'perch_003'], [2.42, 'perch_002'],
      [2.8, 'perch_004'], [3.5, 'perch_005'], [3.62, 'perch_004'], [4.1, 'perch_000']];
    const floorSeq = [[0, 'floor_000'], [0.6, 'floor_002'], [0.85, 'floor_000'], [1.1, 'floor_002'], [1.35, 'floor_000'], [1.7, 'floor_003'], [1.9, 'floor_000'],
      [2.3, 'floor_004'], [2.9, 'floor_006'], [3.3, 'floor_004'], [3.6, 'floor_006'], [4.0, 'floor_007'], [4.5, 'floor_004']];
    const at = (seq, t) => { let f = seq[0][1]; for (const [s, n] of seq) if (t - T0 >= s) f = n; return f; };

    return (t) => {
      h.update(t, 0.1, 5.9);
      const out = E.p(t, 5.9, 6.5, 'in');
      const pp = E.p(t, 0.3, 1.0, 'expo');
      E.set(photo, { x: (1 - pp) * -600, r: -3 * pp, o: E.clamp(pp * 2) * (1 - out) });
      [perch, floor].forEach((l, i) => {
        const p = E.p(t, 0.5 + i * 0.3, 1.2 + i * 0.3, 'back');
        E.set(l.box, { y: (1 - p) * 60 - out * 50, s: 0.85 + 0.15 * p, o: E.clamp(p * 2) * (1 - out) });
        E.pop(l.chip, t, 1.0 + i * 0.3, 5.8);
      });
      perch.term.show(at(perchSeq, t));
      floor.term.show(at(floorSeq, t));
    };
  },
});

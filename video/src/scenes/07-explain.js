// Explain: why the nightly report failed, read from the real Explain section.
// The camera walks the finding: headline, quoted log lines, next step.
E.scene({
  id: 'explain', dur: 7.5,
  build(root) {
    const h = E.headline(root, { x: 110, y: 230, width: 640, kicker: 'new · explain section', title: 'Why did\nit fail?', body: 'Findings built from what the workflow records, and nothing else.' });
    const chips = ['no network service', 'no model', 'same answer every time'].map((c, i) => {
      const n = E.el('div', 'chip', root, `<span style="color:#6f8a2a" class=g>✓</span> ${c}`);
      E.css(n, { left: 110, top: 660 + i * 76, fontFamily: 'JBM, monospace', fontSize: 26, color: '#1f1a17' });
      return n;
    });
    const term = E.term(root, 'nightly_explain');
    const fx = (col) => E.PAD + col * E.CW, fy = (row) => E.BAR + E.PAD + row * E.LH;
    const wide = { fx: term.w / 2, fy: term.h / 2, X: 1325, Y: 560, s: 0.93 };
    const k1 = { fx: fx(62), fy: fy(7.5), X: 960, Y: 560, s: 1.55 };
    const k2 = { fx: fx(62), fy: fy(18), X: 960, Y: 560, s: 1.55 };
    const k3 = { fx: fx(62), fy: fy(22.5), X: 960, Y: 520, s: 1.55 };
    const keys = [[2.2, wide], [2.95, k1], [3.95, k1], [4.55, k2], [5.45, k2], [5.95, k3]];
    const hA = E.hl(term, term.cell(4, 1, 4, 64), '#d7263d');
    const hB = E.hl(term, term.cell(16, 12, 20, 107), '#e3a21a');
    const hC = E.hl(term, term.cell(21, 2, 22, 120), '#6f8a2a');
    const pA = E.pill(root, 'severity · headline · evidence · next step', '#d7263d');
    const pB = E.pill(root, 'it reads the failed pod’s log and quotes the lines that matter', '#e3a21a');
    const pC = E.pill(root, 'and says what to do next', '#6f8a2a');
    [pA, pB, pC].forEach((p) => (p.style.fontSize = '28px'));

    return (t) => {
      h.update(t, 0.15, 2.0);
      chips.forEach((c, i) => E.pop(c, t, 0.9 + i * 0.12, 1.85 + i * 0.04));
      const pin = E.p(t, 0, 0.9, 'expo'), pout = E.p(t, 6.9, 7.5, 'in');
      const cam = E.camPath(t, keys);
      E.cam(term.win, { ...cam, X: cam.X + (1 - pin) * 700, Y: cam.Y - pout * 120, o: E.clamp(pin * 2) * (1 - pout), persp: 2400, ry: (1 - pin) * -30 });
      const a = E.inOut(t, 2.9, 4.0, 0.3), b = E.inOut(t, 4.5, 5.5, 0.3), c = E.inOut(t, 5.95, 7.4, 0.3);
      E.set(hA, { o: a }); E.set(hB, { o: b }); E.set(hC, { o: c });
      const place = (p, row, col, dy) => { const q = E.toStage(cam, fx(col), fy(row)); E.css(p, { left: q.x, top: q.y + dy }); };
      place(pA, 4, 68, -14); E.pop(pA, t, 3.0, 3.9);
      E.css(pB, { left: 960 - pB.offsetWidth / 2, top: 975 }); E.pop(pB, t, 4.6, 5.4);
      E.css(pC, { left: 960 - pC.offsetWidth / 2, top: 975 }); E.pop(pC, t, 6.05, 7.0);
    };
  },
});

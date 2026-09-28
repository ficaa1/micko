// Timeline: the Gantt chart draws itself, then the critical path, waiting
// time and retry bracket are called out, and a running workflow shows the
// live now line.
E.scene({
  id: 'timeline', dur: 8,
  build(root) {
    const h = E.headline(root, { x: 130, y: 70, width: 1700, kicker: 'timeline', title: 'The run as a Gantt chart' });
    const term = E.term(root, 'nightly_timeline', { rows: 14 });
    term.layer('train_timeline');
    const cam = { fx: term.w / 2, fy: E.BAR + E.PAD + 7.5 * E.LH, X: 960, Y: 650, s: 1.55 };
    // Wipe that reveals the bars left to right, with a glowing edge.
    const wipe = E.el('div', '', term.body); E.css(wipe, { top: 5 * E.LH, height: 8 * E.LH, background: term.skin.bg, zIndex: 3 });
    const edge = E.el('div', '', term.body); E.css(edge, { top: 5 * E.LH, height: 8 * E.LH, width: 3, background: '#3f5bb8', boxShadow: '0 0 18px 4px rgba(63,91,184,.7)', zIndex: 4 });
    const hCrit = E.hl(term, term.cell(6, 38, 10, 38), '#e8553b');
    const hWait = E.hl(term, term.cell(9, 73, 9, 76), '#e3a21a');
    const hRetry = E.hl(term, term.cell(7, 60, 7, 110), '#6f7fd6');
    const hNow = E.hl(term, term.cell(4, 121, 13, 121), '#3f5bb8');
    const spot = E.spot(term, term.cell(6, 38, 10, 38));
    const pCrit = E.pill(root, '<span class=g>◆</span> critical path: the chain that set the end time', '#e8553b');
    const pWait = E.pill(root, '<span class=g>░</span> shading: time a step waited to start', '#e3a21a');
    const pRetry = E.pill(root, 'groups are brackets over the time they took', '#6f7fd6');
    const pNow = E.pill(root, 'a live “now” line while it runs', '#3f5bb8');
    const at = (pill, r, c, dx = 0, dy = 0) => { const q = E.toStage(cam, r.x + dx, r.y + dy); E.css(pill, { left: q.x, top: q.y }); };

    return (t) => {
      h.update(t, 0.1, 7.45);
      const pin = E.p(t, 0, 0.9, 'expo'), pout = E.p(t, 7.45, 8, 'in');
      E.cam(term.win, { ...cam, Y: cam.Y + (1 - pin) * 260 - pout * 80, rx: (1 - pin) * 24, persp: 2600, o: E.clamp(pin * 1.8) * (1 - pout) });
      // Draw the chart.
      const x0 = 40 * E.CW, x1 = 122 * E.CW;
      const wx = E.lerp(x0, x1, E.p(t, 0.8, 2.5, 'inOut'));
      E.css(wipe, { left: wx, width: Math.max(0, x1 - wx + 20) });
      E.css(edge, { left: wx - 1 });
      E.set(edge, { o: E.inOut(t, 0.75, 2.4, 0.2) });
      // Switch to the running workflow for the now line.
      const sw = E.p(t, 5.7, 6.2, 'inOut');
      term.wipe('nightly_timeline', 'train_timeline', sw);
      if (sw > 0 && sw < 1) { E.css(edge, { left: sw * 124 * E.CW, top: 0, height: 14 * E.LH }); E.set(edge, { o: 1 }); }
      else E.css(edge, { top: 5 * E.LH, height: 8 * E.LH });
      // Callouts one after another.
      const a = E.inOut(t, 2.7, 3.95, 0.3), b = E.inOut(t, 4.1, 4.85, 0.3), c = E.inOut(t, 4.95, 5.6, 0.3), d = E.inOut(t, 6.3, 7.3, 0.3);
      E.set(hCrit, { o: a }); E.set(hWait, { o: b }); E.set(hRetry, { o: c }); E.set(hNow, { o: d });
      const cur = d > 0 ? term.cell(4, 121, 13, 121) : c > 0 ? term.cell(7, 60, 7, 110) : b > 0 ? term.cell(9, 73, 9, 76) : term.cell(6, 38, 10, 38);
      spot.place(cur); spot.set(Math.max(a, b, c, d) * 0.85);
      at(pCrit, term.cell(10, 38), 0, -30, 40); E.pop(pCrit, t, 2.85, 3.85);
      at(pWait, term.cell(9, 73), 0, -120, 34); E.pop(pWait, t, 4.2, 4.8);
      at(pRetry, term.cell(7, 60), 0, 0, -58); E.pop(pRetry, t, 5.0, 5.55);
      at(pNow, term.cell(13, 100), 0, -250, 36); E.pop(pNow, t, 6.4, 7.25);
    };
  },
});

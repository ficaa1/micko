// Logs and events: lines stream into the workflow-wide log with per-step
// labels and coloured level words, then the live events pane slides in.
E.scene({
  id: 'logs', dur: 6.5,
  build(root) {
    const h = E.headline(root, { x: 110, y: 70, width: 1700, kicker: 'logs & events', title: 'Who said it, and what happened' });
    const logs = E.term(root, 'nightly_logs', { rows: 24, title: 'micko --demo  ·  logs' });
    const ev = E.term(root, 'gate_events', { rows: 15, title: 'micko --demo  ·  events' });
    const cover = E.el('div', '', logs.body); E.css(cover, { left: 0, width: 124 * E.CW, height: 30 * E.LH, background: logs.skin.bg, zIndex: 3 });
    const hSrc = E.hl(logs, logs.cell(4, 1, 23, 16), '#6f7fd6');
    const hErr = [7, 14, 21].map((r) => E.hl(logs, logs.cell(r, 40, r, 44), '#d7263d'));
    const hEv = E.hl(ev, ev.cell(4, 1, 13, 120), '#e3a21a');
    const pills = [
      ['every line labelled with the step that wrote it', '#6f7fd6', 2.1, 3.2],
      ['level words coloured: ERROR, WARN, DEBUG', '#d7263d', 3.25, 4.3],
      ['Kubernetes events stream live while a workflow is open', '#e3a21a', 4.75, 5.95],
    ].map(([txt, c, a, b]) => { const p = E.pill(root, txt, c); p.style.fontSize = '26px'; return { p, a, b }; });

    return (t) => {
      h.update(t, 0.05, 5.95);
      const pin = E.p(t, 0, 0.8, 'expo'), pout = E.p(t, 5.95, 6.5, 'in');
      E.cam(logs.win, { fx: 0, fy: 0, X: 110, Y: 290 + (1 - pin) * 300 - pout * 60, s: 0.95, rx: (1 - pin) * 20, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      // Lines arrive one by one.
      const shown = 4 + Math.floor(E.clamp((t - 0.5) / 1.4) * 22);
      E.css(cover, { top: shown * E.LH });
      // Events window flies in from the right, a little in front.
      const ein = E.p(t, 4.2, 5.0, 'expo');
      E.cam(ev.win, { fx: 0, fy: 0, X: 1010 + (1 - ein) * 1000, Y: 560 - pout * 60, s: 0.78, ry: (1 - ein) * -35, persp: 2400, o: E.clamp(ein * 2) * (1 - pout) });
      E.set(hSrc, { o: E.inOut(t, 2.0, 3.15, 0.3) });
      hErr.forEach((b) => E.set(b, { o: E.inOut(t, 3.25, 4.25, 0.3) }));
      E.set(hEv, { o: E.inOut(t, 4.8, 5.9, 0.3) });
      pills.forEach(({ p, a, b }) => { E.css(p, { left: 110, top: 960 }); E.pop(p, t, a + 0.1, b - 0.1); });
    };
  },
});

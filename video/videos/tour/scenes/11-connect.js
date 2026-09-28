// Connect: the real profile picker with three sample profiles, and how micko
// reaches a cluster: a managed kubectl port-forward, a token from the
// environment or a file, verified TLS, read-only until actions are allowed.
E.scene({
  id: 'connect', dur: 5.5,
  build(root) {
    const h = E.headline(root, { x: 110, y: 170, width: 700, kicker: 'connect', title: 'Point it at\nyour cluster', body: 'One profile per cluster in ~/.config/micko/config.yaml. P switches.' });
    const term = E.term(root, 'picker', { rows: 8, title: 'micko' });
    const cam = { fx: term.w / 2, fy: term.h / 2, X: 1330, Y: 330, s: 0.92 };
    const hl = E.hl(term, term.cell(4, 1, 6, 60), '#3f5bb8');
    const facts = [
      ['kubectl port-forward', 'started for you, closed on exit', '#3f5bb8'],
      ['token from env or file', 'never in a command line', '#e3a21a'],
      ['TLS verified', '--ca-file for your own CA', '#6f8a2a'],
      ['read-only', 'until --allow-actions', '#d7263d'],
    ];
    const cards = facts.map(([a, b, col], i) => {
      const c = E.el('div', 'card', root);
      E.css(c, { left: 790 + (i % 2) * 540, top: 590 + Math.floor(i / 2) * 150, width: 510, height: 124 });
      E.css(E.el('div', '', c), { left: 0, top: 0, width: 12, height: 118, background: col });
      E.css(E.el('div', '', c, a), { left: 34, top: 22, font: '800 27px JBM, monospace', color: '#1f1a17', whiteSpace: 'nowrap' });
      E.css(E.el('div', '', c, b), { left: 34, top: 68, font: '400 21px JBM, monospace', color: '#4a3f36', whiteSpace: 'nowrap' });
      return c;
    });

    return (t) => {
      h.update(t, 0.1, 4.9);
      const pin = E.p(t, 0, 0.8, 'expo'), pout = E.p(t, 4.9, 5.5, 'in');
      E.cam(term.win, { ...cam, Y: cam.Y - (1 - pin) * 300 - pout * 60, rx: (1 - pin) * -20, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      E.set(hl, { o: E.inOut(t, 1.0, 4.7, 0.3) });
      cards.forEach((c, i) => E.pop(c, t, 1.5 + i * 0.25, 4.85));
    };
  },
});

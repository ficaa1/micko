// What micko does, before any detail: four jobs as paper cards, each with a
// thumbnail of the real screen that does it and the keys that get you there.
E.scene({
  id: 'what', dur: 6,
  sfx: [0, 1, 2, 3].map((i) => [0.75 + i * 0.22, 'thump']),
  build(root) {
    const h = E.headline(root, { x: 0, y: 60, width: 1920, align: 'center', kicker: 'what micko does', title: 'Argo Workflows, in your terminal' });
    const jobs = [
      ['Browse', 'list_0', ['↑↓', 'enter'], 'every run: its phase, progress and why it failed', '#3f5bb8'],
      ['Understand', 'nightly_explain', ['T', 'X'], 'a timeline of the run and a plain-words explanation', '#d7263d'],
      ['Follow', 'nightly_logs', ['l', 'E'], 'logs labelled by step, and live Kubernetes events', '#e3a21a'],
      ['Act', 'marks_3', ['a'], 'resume, retry, stop: each one confirmed, none retried', '#6f8a2a'],
    ];
    const W = 400, H = 470, GAP = 40, X0 = (1920 - (4 * W + 3 * GAP)) / 2, Y0 = 320, S = 0.3;
    const cards = jobs.map(([verb, screen, keys, line, col], i) => {
      const c = E.el('div', 'card', root);
      E.css(c, { left: X0 + i * (W + GAP), top: Y0, width: W, height: H });
      const term = E.term(c, screen, { title: 'micko' });
      E.cam(term.win, { fx: 0, fy: 0, X: (W - term.w * S) / 2 - 3, Y: 24, s: S });
      const top = 24 + term.h * S + 26;
      const v = E.el('div', '', c, verb);
      E.css(v, { left: 26, top, font: '800 42px JBM, monospace', color: '#1f1a17', whiteSpace: 'nowrap' });
      const bar = E.el('div', '', c); E.css(bar, { left: 26, top: top + 56, width: 60, height: 6, background: col });
      let kx = W - 26;
      [...keys].reverse().forEach((k) => {
        const n = E.el('div', 'chip', c, k); E.css(n, { top: top + 2, fontSize: 20, padding: '5px 11px' });
        kx -= n.offsetWidth; E.css(n, { left: kx }); kx -= 10;
      });
      const t = E.el('div', 'body', c, line); E.css(t, { left: 26, top: top + 78, width: W - 52, fontSize: 21, lineHeight: 1.45 });
      return { c, rot: [-2, 1.5, -1, 2][i] };
    });

    return (t) => {
      h.update(t, 0.05, 5.35);
      const out = E.p(t, 5.35, 6, 'in');
      cards.forEach(({ c, rot }, i) => {
        const p = E.p(t, 0.45 + i * 0.22, 1.15 + i * 0.22, 'back');
        // Each card is lifted in turn while the others sit, like a hand going over them.
        const lift = E.inOut(t, 1.9 + i * 0.75, 2.5 + i * 0.75, 0.25, 'inOut');
        E.set(c, { y: (1 - p) * 420 - lift * 22 + out * 60, r: rot * (1 - lift) + (1 - p) * rot * 6, s: 1 + lift * 0.04, o: E.clamp(p * 2) * (1 - out) });
        c.style.zIndex = lift > 0.01 ? 2 : 1;
      });
    };
  },
});

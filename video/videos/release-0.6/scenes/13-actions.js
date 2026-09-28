// Marks and bulk actions: rows get marked in the real list, then a designed
// sketch of the menu and the four outcomes every write reports.
E.scene({
  id: 'actions', dur: 6.5,
  sfx: [0, 1, 2].map((k) => [1.0 + k * 0.35, 'click']),
  build(root) {
    const h = E.headline(root, { x: 110, y: 90, width: 700, kicker: 'marks & bulk actions', title: 'Act on many,\nsafely' });
    const term = E.term(root, 'list_0', { rows: 17 });
    ['marks_1', 'marks_2', 'marks_3'].forEach((n) => term.layer(n));
    const hMark = E.hl(term, term.cell(2, 1, 2, 12), '#e8553b');
    const hRows = E.hl(term, term.cell(5, 1, 7, 7), '#e8553b');

    // The menu: only the verbs that apply, with how many each applies to.
    const menu = E.el('div', 'card', root); E.css(menu, { left: 110, top: 470, width: 700, height: 330, padding: 0 });
    E.css(E.el('div', 'kicker', menu, 'a  ·  actions on 3 marked'), { left: 32, top: 26, fontSize: 18, color: '#e8553b', textTransform: 'none', letterSpacing: '.08em' });
    const verbs = [['r', 'retry', 'reruns only what failed'], ['b', 'resubmit', 'a fresh run'], ['d', 'delete', 'asks twice; D to confirm']];
    const vrows = verbs.map(([k, v, d], i) => {
      const r = E.el('div', '', menu); E.css(r, { left: 20, top: 74 + i * 76, width: 660, height: 64, borderRadius: 12 });
      E.css(E.el('div', 'chip', r, k), { left: 12, top: 8, padding: '6px 16px', fontSize: 26, color: '#1f1a17' });
      E.css(E.el('div', '', r, v), { left: 86, top: 14, font: '700 26px JBM, monospace', color: '#1f1a17' });
      E.css(E.el('div', '', r, d), { left: 232, top: 19, font: '400 19px JBM, monospace', color: '#4a3f36' });
      E.css(E.el('div', '', r, '3 of 3'), { left: 575, top: 17, font: '600 22px JBM, monospace', color: '#6f8a2a' });
      return r;
    });

    const outcomes = [['CONFIRMED', 'end state observed', '#6f8a2a'], ['ACCEPTED', 'applied, not yet seen', '#3f5bb8'], ['REFUSED', 'nothing was sent', '#e3a21a'], ['UNKNOWN', 'inspect before acting', '#d7263d']];
    const oc = outcomes.map(([w, d, c], i) => {
      const n = E.el('div', 'card', root); E.css(n, { left: 890 + (i % 2) * 480, top: 650 + Math.floor(i / 2) * 150, width: 450, height: 130 });
      E.css(E.el('div', '', n, w), { left: 28, top: 22, font: '800 36px JBM, monospace', color: c, letterSpacing: '.04em' });
      E.css(E.el('div', '', n, d), { left: 28, top: 76, font: '400 24px JBM, monospace', color: '#4a3f36' });
      return n;
    });
    const foot = E.el('div', '', root, 'off by default: <span class=g style="color:#e8553b">--allow-actions</span> · one request, never retried · every attempt journaled');
    E.css(foot, { left: 110, top: 985, font: '400 26px JBM, monospace', color: '#4a3f36', whiteSpace: 'nowrap' });

    return (t) => {
      h.update(t, 0.1, 5.9);
      const pin = E.p(t, 0, 0.9, 'expo'), pout = E.p(t, 5.95, 6.5, 'in');
      E.cam(term.win, { fx: 0, fy: 0, X: 890 + (1 - pin) * 900, Y: 90, s: 0.84, ry: (1 - pin) * -30, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      term.show(t < 1.0 ? 'list_0' : t < 1.35 ? 'marks_1' : t < 1.7 ? 'marks_2' : 'marks_3');
      E.set(hMark, { o: E.inOut(t, 1.9, 5.8, 0.3) });
      E.set(hRows, { o: E.inOut(t, 1.9, 5.8, 0.3) });
      // Menu, with retry picked.
      const pm = E.p(t, 2.2, 2.8, 'back');
      E.set(menu, { o: E.clamp(pm * 2) * (1 - pout), s: 0.9 + 0.1 * pm, y: (1 - pm) * 40 });
      vrows.forEach((r, i) => {
        const pr = E.p(t, 2.45 + i * 0.1, 2.9 + i * 0.1);
        E.set(r, { o: pr, x: (1 - pr) * -30 });
        r.style.background = i === 0 && t > 3.2 ? 'rgba(63,91,184,.12)' : 'transparent';
        r.style.boxShadow = i === 0 && t > 3.2 ? 'inset 0 0 0 2px rgba(63,91,184,.6)' : 'none';
      });
      oc.forEach((n, i) => E.pop(n, t, 3.6 + i * 0.15, 5.85));
      const pf = E.p(t, 4.4, 4.9);
      E.set(foot, { o: pf * (1 - pout), y: (1 - pf) * 20 });
    };
  },
});

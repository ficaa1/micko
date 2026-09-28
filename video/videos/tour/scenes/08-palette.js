// Command palette: ":" then "cron" typed live, enter opens the cron list, "i"
// opens its info panel. Keycaps under the window echo each key.
E.scene({
  id: 'palette', dur: 6.5,
  sfx: [1.0, 1.4, 1.65, 1.9, 2.15, 2.85, 3.9].map((t) => [t, 'click']),
  build(root) {
    const h = E.headline(root, { x: 110, y: 230, width: 560, kicker: 'command palette', title: 'One key,\nevery view', body: 'Commands rank as you type and tab completes. Cron workflows, templates, the archive, namespaces and profiles.' });
    const term = E.term(root, 'list_0');
    ['palette_00', 'palette_01', 'palette_02', 'palette_03', 'palette_04', 'cron_list', 'cron_info'].forEach((n) => term.layer(n));
    const keys = [[':', 1.0, 'palette_00'], ['c', 1.4, 'palette_01'], ['r', 1.65, 'palette_02'], ['o', 1.9, 'palette_03'], ['n', 2.15, 'palette_04'], ['enter', 2.85, 'cron_list'], ['i', 3.9, 'cron_info']];
    const caps = keys.map(([k], i) => {
      const n = E.el('div', 'chip', root, k);
      E.css(n, { top: 945, left: 760 + i * 92 + (i === 6 ? 70 : 0), minWidth: 76, textAlign: 'center', fontSize: 30, padding: '12px 18px',
        background: 'linear-gradient(180deg,#fbf8f1,#efe4cf)', border: '1px solid rgba(31,26,23,.35)', boxShadow: '0 6px 0 #1f1a17, 0 0 0 transparent', color: '#1f1a17' });
      return n;
    });
    const hPal = E.hl(term, term.cell(2, 1, 4, 80), '#6f7fd6');
    const hCron = E.hl(term, term.cell(3, 1, 6, 120), '#3f5bb8');
    const hInfo = E.hl(term, term.cell(20, 1, 27, 58), '#6f8a2a');
    const pills = [
      [':cron, :tmpl, :cwftmpl, :aw, :ns, :all, :profile', '#6f7fd6', 1.2, 2.8],
      ['cron workflows: schedule, last run, next run, policy', '#3f5bb8', 3.05, 3.85],
      ['the next five runs, computed like the controller does', '#6f8a2a', 4.1, 5.9],
    ].map(([txt, c, a, b]) => { const p = E.pill(root, txt, c); p.style.fontSize = '26px'; return { p, a, b }; });
    const cam = { fx: term.w / 2, fy: term.h / 2, X: 1300, Y: 510, s: 0.93 };

    return (t) => {
      h.update(t, 0.3, 5.95);
      const pin = E.p(t, 0, 1.0, 'expo'), pout = E.p(t, 5.95, 6.5, 'in');
      E.cam(term.win, { ...cam, Y: cam.Y + (1 - pin) * 500 - pout * 80, rx: (1 - pin) * 30, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      let cur = 'list_0';
      keys.forEach(([k, at, scr], i) => {
        if (t >= at) cur = scr;
        // Keycap: pops in, then presses down.
        const press = E.inOut(t, at, at + 0.08, 0.08);
        const pop = E.p(t, at - 0.2, at + 0.15, 'back');
        E.set(caps[i], { y: (1 - pop) * 30 + press * 5, s: 0.8 + 0.2 * pop, o: E.clamp(pop * 2) * (1 - pout) });
        caps[i].style.borderColor = press > 0.5 ? '#3f5bb8' : 'rgba(31,26,23,.35)';
      });
      term.show(cur);
      E.set(hPal, { o: E.inOut(t, 1.2, 2.75, 0.25) });
      E.set(hCron, { o: E.inOut(t, 3.05, 3.8, 0.25) });
      E.set(hInfo, { o: E.inOut(t, 4.1, 5.9, 0.3) });
      pills.forEach(({ p, a, b }) => { E.css(p, { left: 760, top: 850 }); E.pop(p, t, a + 0.1, b - 0.1); });
    };
  },
});

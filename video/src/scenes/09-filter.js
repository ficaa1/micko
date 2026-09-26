// Query filters: the query types itself in large type while the real list
// narrows under it, then the rest of the grammar lands as chips.
E.scene({
  id: 'filter', dur: 5.5,
  build(root) {
    const k = E.el('div', 'kicker', root, 'list filter · a small query language'); E.css(k, { left: 110, top: 90 });
    const q = 'phase=Failed age<3h';
    const big = E.el('div', '', root);
    E.css(big, { left: 110, top: 140, font: '700 96px/1.1 JBM, monospace', whiteSpace: 'pre', color: '#e6e9ff' });
    const col = (i) => (i < 6 ? '#7aa2f7' : i < 12 ? '#f7768e' : i === 12 ? '#e6e9ff' : i < 17 ? '#7aa2f7' : '#e0af68');
    const chars = [...q].map((ch, i) => { const s = E.el('span', 'w', big, E.esc(ch)); s.style.color = col(i); return s; });
    const slash = E.el('span', 'w', big, '/ '); slash.style.color = '#565f89'; big.prepend(slash);
    const caret = E.el('span', 'w', big); E.css(caret, { display: 'inline-block', width: 52, height: 96, background: '#7dcfff', top: 14 });
    const term = E.term(root, 'filter_00', { rows: 17 });
    for (let i = 1; i <= 19; i++) term.layer('filter_' + String(i).padStart(2, '0'));
    const hRows = E.hl(term, term.cell(4, 1, 5, 120), '#f7768e');
    const grammar = ['a|b', '!word', '/regex/', '~fuzzy', 'dur>10m', 'label:k=v', 'tmpl=etl', 'cron=nightly'];
    const chips = grammar.map((g) => { const n = E.el('div', 'chip', root, E.esc(g)); n.style.fontSize = '28px'; n.style.color = '#c0caf5'; return n; });
    let x = 110; chips.forEach((c) => { E.css(c, { left: x, top: 960 }); x += c.offsetWidth + 16; });
    const T0 = 0.9, DT = 0.095;
    // "N shown" from each captured footer: the real count as the query grows.
    const shown = [];
    for (let i = 0; i <= 19; i++) {
      const rows = window.SCREENS['filter_' + String(i).padStart(2, '0')].rows;
      const m = rows[rows.length - 1].map((r) => r[0]).join('').match(/(\d+) shown/);
      shown.push(m ? +m[1] : 12);
    }
    const stat = E.el('div', '', root); E.css(stat, { left: 1330, top: 420, width: 480, textAlign: 'center' });
    const num = E.el('div', '', stat); E.css(num, { left: 0, width: 480, top: 0, font: '700 220px/1 Grotesk, sans-serif', color: '#fff', textAlign: 'center' });
    const lab = E.el('div', 'body', stat, 'workflows shown'); E.css(lab, { left: 0, width: 480, top: 230, textAlign: 'center' });

    return (t) => {
      const pk = E.p(t, 0, 0.5), pout = E.p(t, 4.95, 5.5, 'in');
      E.set(k, { o: pk * (1 - pout), x: (1 - pk) * -30 });
      const n = E.clamp(Math.floor((t - T0) / DT) + 1, 0, 19);
      chars.forEach((s, i) => E.set(s, { o: i < n ? 1 : 0, y: 0 }));
      E.set(slash, { o: E.p(t, 0.5, 0.8) });
      caret.style.opacity = (t < T0 + 19 * DT || (t * 2) % 1 < 0.6) && t > 0.5 ? 1 : 0.15;
      // The caret sits after the last typed char: move it in the flow.
      big.insertBefore(caret, chars[n] || null);
      E.set(big, { o: 1 - pout, y: -pout * 40 });
      const pin = E.p(t, 0.1, 1.0, 'expo');
      E.cam(term.win, { fx: 0, fy: 0, X: 110, Y: 330 + (1 - pin) * 400 - pout * 60, s: 1.0, rx: (1 - pin) * 25, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      term.show('filter_' + String(n).padStart(2, '0'));
      num.textContent = shown[n];
      num.style.color = shown[n] < 12 ? '#f7768e' : '#fff';
      const ps = E.p(t, 0.4, 1.2);
      E.set(stat, { o: ps * (1 - pout), y: (1 - ps) * 40 });
      // A little bump each time the count changes.
      const changed = shown.findIndex((v, i) => i <= n && v === shown[n]);
      const tc = T0 + (changed - 1) * DT;
      E.set(num, { s: 1 + 0.12 * (1 - E.p(t, tc, tc + 0.35)) * (changed > 0 ? 1 : 0) });
      E.set(hRows, { o: E.inOut(t, T0 + 19 * DT + 0.1, 4.9, 0.3) });
      chips.forEach((c, i) => E.pop(c, t, 3.0 + i * 0.08, 4.95));
    };
  },
});

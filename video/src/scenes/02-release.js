// Release card: the version rolls up to 0.6, then what 0.6 brings (the new
// name and Mićko) and what it keeps fan out as pills, and the camera pushes
// through the number.
E.scene({
  id: 'release', dur: 4.5,
  build(root) {
    const kick = E.el('div', 'kicker', root, 'new release · v0.6.0');
    E.css(kick, { left: 0, width: 1920, top: 170, textAlign: 'center', fontSize: 24 });

    const big = E.el('div', 'flow', root);
    E.css(big, { position: 'absolute', left: 0, width: 1920, top: 210, textAlign: 'center', font: '700 400px/1 JBM, monospace', letterSpacing: '-0.04em', color: '#1f1a17', whiteSpace: 'nowrap' });
    big.innerHTML = '<span class="grad">0.</span><span id="rel-col" class="grad" style="display:inline-block;height:1em;overflow:hidden;vertical-align:top;position:relative;padding-right:.07em"><span id="rel-strip" class="grad" style="display:block;position:relative;line-height:1">0<br>1<br>2<br>3<br>4<br>5<br>6</span></span>';
    const strip = big.querySelector('#rel-strip');

    // Row one is what is new in 0.6, in rosella colours; row two what it keeps.
    const news = [['argo-tui → micko', '#d7263d'], ['Mićko, the mascot', '#e8553b'], ['<span class=g>--mascot</span>', '#e3a21a']];
    const keeps = ['Timeline', 'Explain', 'Events', 'Command palette', 'Query filters', '13 skins', 'Bulk actions'];
    const pill = (html, dot, big) => {
      const n = E.el('div', 'chip', root, `<span style="color:${dot}">●</span> ${html}`);
      E.css(n, { fontFamily: 'JBM, monospace', fontSize: big ? 32 : 24, color: big ? '#1f1a17' : '#4a3f36' });
      if (big) { n.style.borderColor = dot + '88'; n.style.background = dot + '1f'; }
      return n;
    };
    const row1 = news.map(([h, c]) => pill(h, c, true));
    const row2 = keeps.map((h) => pill(h, '#8a7b69', false));
    [[row1, 690], [row2, 800]].forEach(([r, top]) => {
      const widths = r.map((p) => p.offsetWidth), gap = 18;
      let x = 960 - (widths.reduce((a, b) => a + b, 0) + gap * (r.length - 1)) / 2;
      r.forEach((p, i) => { E.css(p, { left: x, top }); x += widths[i] + gap; });
    });
    const pills = [...row1, ...row2];

    return (t) => {
      const pin = E.p(t, 0, 0.5);
      E.set(kick, { o: E.p(t, 0.2, 0.7) * (1 - E.p(t, 3.9, 4.3)), y: (1 - E.p(t, 0.2, 0.7)) * 20 });
      // Roll 0 -> 6 with an overshoot settle.
      const roll = E.p(t, 0.25, 1.45, 'spring') * 6;
      strip.style.transform = `translateY(${-roll}em)`;
      const out = E.p(t, 3.85, 4.5, 'in');
      E.set(big, { s: (0.85 + 0.15 * pin) * (1 + out * 2.4), o: pin * (1 - out), blur: out * 14 });
      big.style.transformOrigin = '960px 200px';
      pills.forEach((p, i) => E.pop(p, t, 1.35 + i * (i < 3 ? 0.12 : 0.05) + (i >= 3 ? 0.3 : 0), 3.6 + (i % 5) * 0.03));
    };
  },
});

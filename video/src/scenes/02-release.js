// Release card: the version rolls up to 0.5, then the headline features fan
// out as pills, and the camera pushes through the number.
E.scene({
  id: 'release', dur: 4.5,
  build(root) {
    const kick = E.el('div', 'kicker', root, 'new release · v0.5.0');
    E.css(kick, { left: 0, width: 1920, top: 190, textAlign: 'center', fontSize: 24 });

    const big = E.el('div', 'flow', root);
    E.css(big, { position: 'absolute', left: 0, width: 1920, top: 230, textAlign: 'center', font: '700 400px/1 Grotesk, sans-serif', letterSpacing: '-0.04em', color: '#fff', whiteSpace: 'nowrap' });
    big.innerHTML = '<span class="grad">0.</span><span id="rel-col" style="display:inline-block;height:1em;overflow:hidden;vertical-align:top;position:relative"><span id="rel-strip" style="display:block;position:relative">0<br>1<br>2<br>3<br>4<br>5</span></span>';
    const strip = big.querySelector('#rel-strip');
    const col = big.querySelector('#rel-col');
    [...strip.childNodes].forEach(() => {});
    strip.style.lineHeight = '1';
    col.classList.add('grad');
    strip.classList.add('grad');

    const feats = ['Timeline', 'Explain', 'Events', 'Command palette', 'Query filters', '13 skins', 'Bulk actions', 'Cron & templates', 'Archive', 'All namespaces'];
    const colors = ['#7dcfff', '#f7768e', '#e0af68', '#bb9af7', '#7aa2f7', '#9ece6a', '#ff9e64', '#7dcfff', '#bb9af7', '#9ece6a'];
    const pills = feats.map((f, i) => {
      const n = E.el('div', 'chip', root, `<span style="color:${colors[i]}">●</span> ${f}`);
      n.style.fontFamily = 'Inter, sans-serif'; n.style.fontSize = '26px'; n.style.color = '#e6e9ff';
      return n;
    });
    // Two centred rows; measure after fonts load (build runs after E.ready()).
    const rows = [pills.slice(0, 5), pills.slice(5)];
    rows.forEach((r, ri) => {
      const widths = r.map((p) => p.offsetWidth), gap = 18;
      let x = 960 - (widths.reduce((a, b) => a + b, 0) + gap * (r.length - 1)) / 2;
      r.forEach((p, i) => { E.css(p, { left: x, top: 720 + ri * 86 }); x += widths[i] + gap; });
    });

    return (t) => {
      const pin = E.p(t, 0, 0.5);
      E.set(kick, { o: E.p(t, 0.2, 0.7) * (1 - E.p(t, 3.9, 4.3)), y: (1 - E.p(t, 0.2, 0.7)) * 20 });
      // Roll 0 -> 5 with an overshoot settle.
      const roll = E.p(t, 0.25, 1.45, 'spring') * 5;
      strip.style.transform = `translateY(${-roll}em)`;
      const out = E.p(t, 3.85, 4.5, 'in');
      E.set(big, { s: (0.85 + 0.15 * pin) * (1 + out * 2.4), o: pin * (1 - out), blur: out * 14 });
      big.style.transformOrigin = '960px 200px';
      pills.forEach((p, i) => {
        E.pop(p, t, 1.35 + i * 0.07, 3.6 + (i % 5) * 0.03);
      });
    };
  },
});

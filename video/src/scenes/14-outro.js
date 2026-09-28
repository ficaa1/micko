// Outro: Mićko, the wordmark and version, the two commands to try it, a
// ticker of what shipped, then fade to black.
E.scene({
  id: 'outro', dur: 7,
  build(root) {
    const mark = E.el('div', '', root);
    E.css(mark, { left: 0, width: 1920, top: 170, textAlign: 'center', font: '800 150px/1 JBM, monospace', letterSpacing: '-0.03em', whiteSpace: 'pre' });
    const letters = [...'micko'].map((ch) => E.el('span', 'w grad', mark, ch));
    // Mićko himself, as a round avatar left of the wordmark.
    const textW = letters.reduce((a, s) => a + s.offsetWidth, 0);
    const av = E.el('div', '', root);
    E.css(av, { left: 960 - textW / 2 - 60 - 150, top: 160, width: 150, height: 150, borderRadius: '50%', overflow: 'hidden', border: '5px solid #f2efe9', boxShadow: '0 16px 40px rgba(0,0,0,.5)' });
    const avImg = E.el('img', '', av); avImg.src = 'photos/micko-cage.jpg';
    E.css(avImg, { left: 0, top: 0, width: '100%', height: '100%', objectFit: 'cover', objectPosition: '50% 18%' });
    E.css(mark, { left: 75 });
    const ver = E.el('div', 'chip', root, 'v0.6.0');
    E.css(ver, { top: 350, fontSize: 30, color: '#0a0b14', background: 'linear-gradient(90deg,#f7768e,#ff9e64)', border: 'none', padding: '10px 26px', fontWeight: 800 });

    const card = E.el('div', 'card', root); E.css(card, { left: 360, top: 470, width: 1200, height: 250 });
    const lines = ['gh release download v0.6.0 --repo ficaa1/micko', 'micko --demo --mascot'];
    const ls = lines.map((l, i) => {
      const n = E.el('div', '', card); E.css(n, { left: 44, top: 52 + i * 84, font: '500 34px JBM, monospace', color: '#c0caf5', whiteSpace: 'pre' });
      return n;
    });
    const cmt = E.el('div', '', card, '# no cluster needed: synthetic data, no writes');
    E.css(cmt, { left: 44, top: 196, font: '400 24px JBM, monospace', color: '#565f89', whiteSpace: 'pre' });

    const feats = 'Mićko the mascot  ·  Timeline  ·  Explain  ·  Events  ·  Command palette  ·  Query filters  ·  13 skins  ·  Bulk actions  ·  Cron workflows  ·  Templates  ·  Archive  ·  All namespaces  ·  ';
    const band = E.el('div', '', root); E.css(band, { left: 0, top: 840, width: 1920, height: 70, overflow: 'hidden',
      maskImage: 'linear-gradient(90deg,transparent,#000 14%,#000 86%,transparent)', webkitMaskImage: 'linear-gradient(90deg,transparent,#000 14%,#000 86%,transparent)' });
    const tick = E.el('div', '', band, E.esc(feats + feats + feats));
    E.css(tick, { top: 10, left: 0, font: '600 34px Inter, sans-serif', color: '#a9b1d6', whiteSpace: 'nowrap' });
    const tw = tick.offsetWidth / 3;
    const fade = E.el('div', '', root); E.css(fade, { left: 0, top: 0, width: 1920, height: 1080, background: '#000', zIndex: 10 });

    return (t) => {
      letters.forEach((s, i) => {
        const p = E.p(t, 0.1 + i * 0.05, 0.7 + i * 0.05, 'back');
        E.set(s, { y: (1 - p) * 90, o: E.clamp(p * 1.5), blur: (1 - E.clamp(p)) * 10 });
      });
      E.css(ver, { left: 960 - ver.offsetWidth / 2 });
      E.pop(ver, t, 0.75);
      E.pop(av, t, 0.35);
      const pc = E.p(t, 1.0, 1.6);
      E.set(card, { o: pc, y: (1 - pc) * 50 });
      // Type the commands, one after the other.
      const typed = (l, a, cps) => l.slice(0, E.clamp(Math.floor((t - a) * cps), 0, l.length));
      ls[0].innerHTML = '<span style="color:#9ece6a">$</span> ' + E.esc(typed(lines[0], 1.5, 38)) + (t > 1.5 && t < 2.9 ? '<span style="color:#7dcfff">▌</span>' : '');
      ls[1].innerHTML = t > 2.9 ? '<span style="color:#9ece6a">$</span> ' + E.esc(typed(lines[1], 3.0, 28)) + ((t * 2) % 1 < 0.6 ? '<span style="color:#7dcfff">▌</span>' : '') : '';
      E.set(cmt, { o: E.p(t, 3.7, 4.2) });
      const pt = E.p(t, 1.8, 2.4);
      E.set(tick, { x: -((t * 110) % tw), o: pt });
      E.set(fade, { o: E.p(t, 6.1, 7, 'inOut') });
    };
  },
});

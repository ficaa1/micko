// Skins: every truecolor skin on the same real list screen, as a tilted wall
// that flips in tile by tile and drifts past.
E.scene({
  id: 'skins', dur: 7,
  build(root) {
    const h = E.headline(root, { x: 0, y: 70, width: 1920, align: 'center', kicker: '', title: '13 truecolor skins' });
    const sub = E.el('div', 'body', root, 'or <span class=g style="color:#3f5bb8">--skin auto</span> picks dark or light from your terminal');
    E.css(sub, { left: 0, width: 1920, top: 200, textAlign: 'center' });
    const wall = E.el('div', '', root); E.css(wall, { left: 0, top: 0, width: 1920, height: 1080 });
    const names = ['catppuccin-mocha', 'gruvbox-dark', 'nord', 'dracula', 'tokyo-night', 'solarized-dark', 'one-dark',
      'rose-pine', 'monokai', 'catppuccin-latte', 'gruvbox-light', 'solarized-light', 'rose-pine-dawn'];
    const S = 0.36, GX = 450, GY = 330;
    const tiles = names.map((n, i) => {
      const term = E.term(wall, 'skin_' + n, { title: n });
      const lab = E.el('div', '', wall, n);
      E.css(lab, { font: '600 22px JBM, monospace', color: '#1f1a17', whiteSpace: 'nowrap' });
      const r = i < 7 ? 0 : 1, c = i < 7 ? i : i - 7 + 0.5;
      return { term, lab, x: 80 + c * GX, y: 300 + r * GY, i };
    });

    return (t) => {
      h.update(t, 0.2, 6.4);
      const ps = E.p(t, 0.6, 1.2);
      E.set(sub, { o: ps * (1 - E.p(t, 6.4, 6.85)), y: (1 - ps) * 20 });
      // The wall drifts left and tilts; it recedes at the end.
      const pan = E.lerp(260, -900, E.p(t, 0, 7, 'inOut'));
      const out = E.p(t, 6.3, 7, 'in');
      wall.style.transformOrigin = '960px 600px';
      wall.style.transform = `perspective(2000px) rotateY(${-16 + out * 10}deg) rotateX(8deg) translateX(${pan}px) translateZ(${-out * 600}px)`;
      wall.style.opacity = 1 - out;
      tiles.forEach(({ term, lab, x, y, i }) => {
        const p = E.p(t, 0.25 + i * 0.1, 1.05 + i * 0.1, 'back');
        E.cam(term.win, { fx: term.w / 2, fy: term.h / 2, X: x + term.w * S / 2, Y: y + term.h * S / 2 + (1 - p) * 80, s: S * (0.7 + 0.3 * p), ry: (1 - E.clamp(p)) * 80, persp: 1400, o: E.clamp(p * 2) });
        E.css(lab, { left: x + 4, top: y + term.h * S + 14 });
        E.set(lab, { o: E.clamp(p * 2), y: (1 - p) * 20 });
      });
    };
  },
});

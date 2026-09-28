// Meet Mićko: the owner's photos land one by one as a collage, then the one
// where he rests his beak on a monitor comes forward, which is the pose the
// ASCII mascot takes on the pane (next scene).
E.scene({
  id: 'bird', dur: 8,
  build(root) {
    const h = E.headline(root, { x: 110, y: 290, width: 620, kicker: 'why the new name', title: 'Meet\nMićko', body: 'An eastern rosella, and the reason argo-tui is now micko.' });
    // [file, card w, card h, centre x, centre y, rotation, object-position]
    const P = [
      ['micko-cage.jpg', 300, 420, 920, 300, -7, '50% 30%'],
      ['micko-fluffy.jpg', 300, 400, 1235, 275, 5, '50% 40%'],
      ['micko-floor.jpg', 300, 400, 1560, 330, -4, '60% 50%'],
      ['micko-perch.jpg', 440, 330, 1010, 760, 4, '50% 35%'],
      ['micko-monitor.jpg', 440, 330, 1460, 745, -5, '65% 55%'],
    ];
    let cards0 = 0;
    const cards = P.map(([f, w, hh, x, y, r, pos]) => {
      const c = E.el('div', '', root);
      E.css(c, { left: x - w / 2, top: y - hh / 2, width: w, height: hh, borderRadius: 6, background: '#fbf8f1',
        border: '10px solid #fbf8f1', boxShadow: '0 0 0 2.5px #1f1a17, 9px 9px 0 2.5px #1f1a17' });
      const img = E.el('img', '', c); img.src = 'photos/' + f;
      E.css(img, { left: 0, top: 0, width: '100%', height: '100%', objectFit: 'cover', objectPosition: pos });
      // Two strips of washi tape hold each print down.
      const i = cards0++;
      [[-18, 26, -9, '#f2c14e'], [-16, w - 150, 7, '#6f7fd6']].forEach(([ty, tx, rot, col], j) => {
        if ((i + j) % 3 === 2) return;
        const tp = E.el('div', 'tape', c); E.css(tp, { top: ty, left: tx, background: col, transform: `rotate(${rot}deg)`, zIndex: 2 });
      });
      return { c, x, y, r, w, hh };
    });
    const hero = cards[4];
    const cap = E.el('div', 'pill', root, 'he rests his beak on the monitor…', '#e8553b');
    cap.style.background = '#e8553b'; cap.style.fontSize = '28px';

    return (t) => {
      h.update(t, 0.2, 7.35);
      const heroP = E.p(t, 5.0, 5.9, 'inOut');
      const out = E.p(t, 7.4, 8, 'in');
      cards.forEach(({ c, r }, i) => {
        // Each photo drops in from above with a spin, 0.45 s apart.
        const p = E.p(t, 0.5 + i * 0.45, 1.25 + i * 0.45, 'back');
        const isHero = i === 4;
        const dim = isHero ? 1 : 1 - heroP * 0.65;
        let x = 0, y = (1 - p) * -520, rot = r + (1 - p) * (i % 2 ? 25 : -25), s = 0.9 + 0.1 * p;
        if (isHero) {
          // Forward and centred on the right half, level.
          x = heroP * (1300 - hero.x); y += heroP * (520 - hero.y); rot = E.lerp(rot, 0, heroP); s *= 1 + heroP * 0.75;
        }
        E.set(c, { x, y: y - out * 60, r: rot, s, o: E.clamp(p * 2) * (1 - out) });
        c.style.filter = dim < 1 ? `grayscale(${1 - dim}) opacity(${0.35 + 0.65 * dim})` : 'none';
        c.style.zIndex = isHero ? 3 : 1;
      });
      E.css(cap, { left: 1300 - cap.offsetWidth / 2, top: 520 + (hero.hh * 1.75) / 2 + 30, zIndex: 4 });
      E.pop(cap, t, 5.7, 7.35);
    };
  },
});

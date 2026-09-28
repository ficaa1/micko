// The mascot: the real 40-row capture with --mascot. The camera goes in on
// the ASCII Mićko next to the photo he is drawn from, then the window cycles
// skins to show he takes his colours from each one.
E.scene({
  id: 'mascot', dur: 7,
  build(root) {
    const h = E.headline(root, { x: 110, y: 90, width: 760, kicker: 'new · --mascot', title: 'Now he perches\non your pane' });
    const skins = ['catppuccin-latte', 'gruvbox-dark', 'dracula', 'rose-pine-dawn', 'nord', 'gruvbox-light'];
    const term = E.term(root, 'mascot_list', { rows: 40 });
    skins.forEach((s) => term.layer('mascot_skin_' + s));

    // Where Mićko sits: the non-blank cells above the pane's top border.
    const rows = window.SCREENS.mascot_list.rows.map((r) => [...r.map((x) => x[0]).join('')]);
    const border = rows.findIndex((r) => r.includes('╭'));
    let c0 = 999, c1 = 0;
    for (let r = 1; r < border; r++) rows[r].forEach((ch, c) => { if (ch.trim()) { c0 = Math.min(c0, c); c1 = Math.max(c1, c); } });
    const bird = term.cell(1, c0, border, c1);
    const hl = E.hl(term, bird, '#e8553b');

    const photo = E.el('div', '', root);
    E.css(photo, { left: 110, top: 470, width: 560, height: 420, borderRadius: 6, background: '#fbf8f1', border: '10px solid #fbf8f1', boxShadow: '0 0 0 2.5px #1f1a17, 9px 9px 0 2.5px #1f1a17' });
    const img = E.el('img', '', photo); img.src = 'photos/micko-monitor.jpg';
    E.css(img, { left: 0, top: 0, width: '100%', height: '100%', objectFit: 'cover', objectPosition: '65% 55%' });
    E.css(E.el('div', 'tape', photo), { top: -18, left: 210, background: '#f2c14e', transform: 'rotate(-4deg)', zIndex: 2 });

    const S = 0.8;
    const wide = { fx: term.w / 2, fy: term.h / 2, X: 1390, Y: 560, s: S };
    const close = { fx: bird.x + bird.w / 2, fy: bird.y + bird.h / 2, X: 1350, Y: 640, s: 2.2 };
    // The close-up crops the window to a lens around him, so the zoom never covers the headline.
    const M = 56, lens = { t: bird.y - M * 0.7, l: bird.x - M, r: term.w - (bird.x + bird.w + M), b: term.h - (bird.y + bird.h + M * 0.7) };
    const pPose = E.pill(root, 'the same pose, in ASCII', '#e8553b'); pPose.style.fontSize = '28px';
    const lab = E.el('div', 'chip', root); E.css(lab, { top: 980, fontSize: 26, color: '#1f1a17', fontFamily: 'JBM, monospace' });
    const how = E.el('div', '', root, '<span class=g style="color:#e8553b">--mascot</span> · <span class=g style="color:#e8553b">:mascot</span> · <span class=g style="color:#e8553b">mascot: true</span><br><span style="color:#8a7b69">off by default · on terminals of 80×40 or larger</span>');
    E.css(how, { left: 110, top: 930, font: '400 26px/1.5 JBM, monospace', color: '#4a3f36', whiteSpace: 'nowrap' });

    return (t) => {
      h.update(t, 0.1, 6.45);
      const pin = E.p(t, 0, 0.9, 'expo'), pout = E.p(t, 6.45, 7, 'in');
      const z = t < 2.3 ? E.p(t, 1.3, 2.1, 'inOut') : 1 - E.p(t, 3.3, 3.95, 'inOut');
      const cam = E.mixCam(wide, close, z);
      E.cam(term.win, { ...cam, X: cam.X + (1 - pin) * 800, o: E.clamp(pin * 2) * (1 - pout), ry: (1 - pin) * -30, persp: 2400 });
      term.win.style.clipPath = z > 0.001 ? `inset(${lens.t * z}px ${lens.r * z}px ${lens.b * z}px ${lens.l * z}px round ${14 + 6 * z}px)` : '';
      // Photo slides in from the left beside him.
      const pp = E.p(t, 0.5, 1.3, 'expo');
      E.set(photo, { x: (1 - pp) * -700, r: -3 * pp, o: E.clamp(pp * 2) * (1 - pout) });
      E.set(hl, { o: E.inOut(t, 2.0, 3.3, 0.3) });
      E.css(pPose, { left: 1350 - pPose.offsetWidth / 2, top: 640 + (bird.h / 2 + M * 0.7) * 2.2 + 26 });
      E.pop(pPose, t, 2.1, 3.25);
      // Skins: one every 0.42 s from 4.0 s.
      const k = Math.floor((t - 4.0) / 0.42);
      const cur = t < 4.0 ? 'mascot_list' : 'mascot_skin_' + skins[Math.min(k, skins.length - 1)];
      term.show(cur);
      lab.textContent = '--skin ' + (t < 4.0 ? 'monokai' : skins[Math.min(k, skins.length - 1)]);
      E.css(lab, { left: 1390 - lab.offsetWidth / 2 });
      E.set(lab, { o: E.inOut(t, 3.95, 6.3, 0.2) });
      const ph = E.p(t, 4.2, 4.8);
      E.set(how, { o: ph * (1 - pout), y: (1 - ph) * 20 });
    };
  },
});

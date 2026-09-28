// Intro: the prompt types `micko --demo`, the wordmark assembles, the
// tagline and the phase glyphs follow, then everything pushes toward the camera.
E.scene({
  id: 'intro', dur: 6,
  sfx: E.typing(0.3, 12, 0.9 / 12),
  build(root) {
    const prompt = E.el('div', '', root);
    E.css(prompt, { left: 0, width: 1920, top: 500, textAlign: 'center', font: '500 46px JBM, monospace', color: '#1f1a17', whiteSpace: 'pre' });
    const typed = E.el('span', '', prompt); typed.style.position = 'static';
    const caret = E.el('span', '', prompt); E.css(caret, { position: 'relative', display: 'inline-block', width: 26, height: 52, top: 8, marginLeft: 4, background: '#3f5bb8' });

    const mark = E.el('div', '', root);
    E.css(mark, { left: 0, width: 1920, top: 330, textAlign: 'center', font: '800 220px/1 JBM, monospace', letterSpacing: '-0.03em', whiteSpace: 'pre' });
    const letters = [...'micko'].map((ch) => E.el('span', 'w grad', mark, ch));
    const cur = E.el('span', 'w', mark); E.css(cur, { display: 'inline-block', width: 110, height: 186, marginLeft: 18, background: 'linear-gradient(180deg,#d7263d,#e8553b)', top: 24 });

    const tag = E.el('div', 'body', root);
    E.css(tag, { left: 0, width: 1920, top: 590, textAlign: 'center', fontSize: 40, color: '#1f1a17' });
    const tagWords = E.words(tag, 'A keyboard-first terminal UI for Argo Workflows');

    const glyphs = [['✓', 'Succeeded', '#6f8a2a'], ['●', 'Running', '#3f5bb8'], ['◐', 'Suspended', '#e3a21a'], ['✗', 'Failed', '#d7263d'], ['○', 'Pending', '#4a3f36']];
    const row = E.el('div', '', root); E.css(row, { left: 0, top: 710, width: 1920, height: 80 });
    const chips = glyphs.map(([g, w, c], i) => {
      const n = E.el('div', 'chip', row, `<span class=g style="color:${c}">${g}</span> ${w}`);
      E.css(n, { left: 960 - (5 * 250) / 2 + i * 250 + 10, top: 0, width: 230, textAlign: 'center', color: '#1f1a17', fontSize: 24 });
      return n;
    });

    const CMD = 'micko --demo';
    const line = (t) => CMD.slice(0, Math.floor(E.clamp((t - 0.3) / 0.9) * CMD.length));

    return (t) => {
      // 1. prompt: old name, deleted, new name; then it lifts away.
      typed.textContent = '❯ ' + line(t);
      caret.style.opacity = t < 1.7 ? ((t * 2.2) % 1 < 0.6 ? 1 : 0.15) : 1;
      const up = E.p(t, 1.75, 2.15, 'in');
      E.set(prompt, { y: -up * 60, o: E.p(t, 0, 0.3) * (1 - up), blur: up * 6 });
      // 2. wordmark letters drop in with a stagger.
      letters.forEach((s, i) => {
        const p = E.p(t, 1.95 + i * 0.07, 2.55 + i * 0.07, 'back');
        E.set(s, { y: (1 - p) * -120, o: E.clamp(p * 1.5), blur: (1 - E.clamp(p)) * 12 });
      });
      const pc = E.p(t, 2.4, 2.75, 'back');
      E.set(cur, { sy: pc, o: pc > 0.01 ? ((t - 2.75) * 2.2 % 1 < 0.6 || t < 2.75 ? 1 : 0.2) : 0 });
      // 3. tagline and glyph chips.
      E.revealWords(tagWords, t, 2.95, 0.07, 0.55);
      chips.forEach((c, i) => E.pop(c, t, 3.8 + i * 0.1));
      // 4. push through toward the viewer.
      const out = E.p(t, 5.35, 6, 'in');
      E.set(root, { s: 1 + out * 0.35, o: 1 - out, blur: out * 10 });
      root.style.transformOrigin = '960px 480px';
    };
  },
});

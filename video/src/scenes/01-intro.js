// Intro: a prompt types the command, the wordmark assembles, the tagline and
// the phase glyphs follow, then everything pushes toward the camera.
E.scene({
  id: 'intro', dur: 6,
  build(root) {
    const cmd = '❯ argo-tui --demo';
    const prompt = E.el('div', '', root);
    E.css(prompt, { left: 0, width: 1920, top: 500, textAlign: 'center', font: '500 46px JBM, monospace', color: '#c0caf5', whiteSpace: 'pre' });
    const typed = E.el('span', '', prompt); typed.style.position = 'static';
    const caret = E.el('span', '', prompt); E.css(caret, { position: 'relative', display: 'inline-block', width: 26, height: 52, top: 8, marginLeft: 4, background: '#7aa2f7' });

    const mark = E.el('div', '', root);
    E.css(mark, { left: 0, width: 1920, top: 330, textAlign: 'center', font: '800 200px/1 JBM, monospace', letterSpacing: '-0.03em', whiteSpace: 'pre' });
    const letters = [...'argo-tui'].map((ch) => { const s = E.el('span', 'w grad', mark, ch); return s; });
    const cur = E.el('span', 'w', mark); E.css(cur, { display: 'inline-block', width: 100, height: 170, marginLeft: 18, background: 'linear-gradient(180deg,#7dcfff,#7aa2f7)', top: 22 });

    const tag = E.el('div', 'body', root);
    E.css(tag, { left: 0, width: 1920, top: 580, textAlign: 'center', fontSize: 40, color: '#c0caf5' });
    const tagWords = E.words(tag, 'A keyboard-first terminal UI for Argo Workflows');

    const glyphs = [['✓', 'Succeeded', '#9ece6a'], ['●', 'Running', '#7dcfff'], ['◐', 'Suspended', '#e0af68'], ['✗', 'Failed', '#f7768e'], ['○', 'Pending', '#a9b1d6']];
    const row = E.el('div', '', root); E.css(row, { left: 0, top: 700, width: 1920, height: 80 });
    const chips = glyphs.map(([g, w, c], i) => {
      const n = E.el('div', 'chip', row, `<span style="color:${c}">${g}</span> ${w}`);
      E.css(n, { left: 960 - (5 * 250) / 2 + i * 250 + 10, top: 0, width: 230, textAlign: 'center', color: '#c0caf5', fontSize: 24 });
      return n;
    });

    return (t) => {
      // 1. prompt types, then lifts away.
      const nType = Math.floor(E.clamp((t - 0.35) / 1.1) * [...cmd].length);
      typed.textContent = [...cmd].slice(0, nType).join('');
      caret.style.opacity = t < 1.5 ? ((t * 2.2) % 1 < 0.6 ? 1 : 0.15) : 1;
      const up = E.p(t, 1.65, 2.1, 'in');
      E.set(prompt, { y: -up * 60, o: E.p(t, 0, 0.3) * (1 - up), blur: up * 6 });
      // 2. wordmark letters drop in with a stagger.
      letters.forEach((s, i) => {
        const p = E.p(t, 1.9 + i * 0.06, 2.5 + i * 0.06, 'back');
        E.set(s, { y: (1 - p) * -120, o: E.clamp(p * 1.5), blur: (1 - E.clamp(p)) * 12 });
      });
      const pc = E.p(t, 2.45, 2.8, 'back');
      E.set(cur, { sy: pc, o: pc > 0.01 ? ((t - 2.8) * 2.2 % 1 < 0.6 || t < 2.8 ? 1 : 0.2) : 0 });
      // 3. tagline and glyph chips.
      E.revealWords(tagWords, t, 3.0, 0.07, 0.55);
      chips.forEach((c, i) => E.pop(c, t, 3.9 + i * 0.1));
      // 4. push through toward the viewer.
      const out = E.p(t, 5.35, 6, 'in');
      E.set(root, { s: 1 + out * 0.35, o: 1 - out, blur: out * 10 });
      root.style.transformOrigin = '960px 480px';
    };
  },
});

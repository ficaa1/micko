// Title card: the prompt types the command, the wordmark drops in letter by
// letter, the tagline follows word by word.
E.scene({
  id: 'title', dur: 4.5,
  sfx: E.typing(0.3, 10, 0.07),               // key clicks under the typing (soundtrack)
  build(root) {
    const cmd = 'yourapp --demo';
    const prompt = E.el('div', '', root);
    E.css(prompt, { left: 0, width: 1920, top: 500, textAlign: 'center', font: '500 46px JBM, monospace', whiteSpace: 'pre' });
    const mark = E.el('div', '', root);
    E.css(mark, { left: 0, width: 1920, top: 330, textAlign: 'center', font: '800 200px/1 JBM, monospace', whiteSpace: 'pre' });
    const letters = [...'yourapp'].map((ch) => E.el('span', 'w grad', mark, ch));
    const tag = E.el('div', 'body', root);
    E.css(tag, { left: 0, width: 1920, top: 580, textAlign: 'center', fontSize: 38, color: 'var(--ink)' });
    const words = E.words(tag, 'One line that says what it is');
    return (t) => {
      prompt.textContent = '❯ ' + cmd.slice(0, Math.floor(E.clamp((t - 0.3) / 0.8) * cmd.length));
      E.set(prompt, { o: 1 - E.p(t, 1.4, 1.8), y: -E.p(t, 1.4, 1.8) * 50 });
      letters.forEach((s, i) => { const p = E.p(t, 1.6 + i * 0.06, 2.2 + i * 0.06, 'back'); E.set(s, { y: (1 - p) * -110, o: E.clamp(p * 1.5) }); });
      E.revealWords(words, t, 2.4, 0.07);
      const out = E.p(t, 4.0, 4.5, 'in');
      E.set(root, { o: 1 - out });
    };
  },
});

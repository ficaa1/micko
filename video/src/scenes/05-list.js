// The workflow list: the window swings in, the cursor walks down, then the
// phase and progress columns are called out.
E.scene({
  id: 'list', dur: 7,
  build(root) {
    const h = E.headline(root, { x: 110, y: 250, width: 600, kicker: 'the workflow list', title: 'Every run,\nat a glance', body: 'Phase as a glyph, a word and a colour. Progress bars. The reason a run failed, right in the row.' });
    const term = E.term(root, 'list_0');
    const phase = E.hl(term, term.cell(3, 49, 15, 67), '#7dcfff');
    const prog = E.hl(term, term.cell(3, 84, 15, 97), '#bb9af7');
    const spot = E.spot(term, term.cell(3, 49, 15, 67));
    const p1 = E.pill(root, '<span class=g>✓ ● ◐ ✗ ○</span>  glyph · word · colour', '#7dcfff');
    const p2 = E.pill(root, 'progress on every row', '#bb9af7');
    const wide = { fx: term.w / 2, fy: term.h / 2, X: 1300, Y: 560, s: 0.93 };

    return (t) => {
      h.update(t, 0.35, 6.4);
      // Fly in from the right with a swing.
      const pin = E.p(t, 0, 1.1, 'expo');
      const pout = E.p(t, 6.35, 7, 'in');
      E.cam(term.win, { ...wide, X: E.lerp(2400, 1300, pin) - pout * 300, ry: (1 - pin) * -38 - 6 * (1 - E.p(t, 1.1, 3)), persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      // Cursor walks down the list.
      term.show(t < 1.9 ? 'list_0' : t < 2.25 ? 'list_1' : t < 2.6 ? 'list_2' : 'list_3');
      // Phase column, then progress column.
      const a = E.inOut(t, 3.0, 4.55, 0.35), b = E.inOut(t, 4.7, 6.1, 0.35);
      E.set(phase, { o: a });
      E.set(prog, { o: b });
      spot.place(b > a ? term.cell(3, 84, 15, 97) : term.cell(3, 49, 15, 67));
      spot.set(Math.max(a, b) * 0.9);
      // Pills sit under the highlighted column, in stage coordinates.
      const sx = 1300 - term.w / 2 * 0.93, sy = 560 - term.h / 2 * 0.93;
      const r1 = term.cell(15, 49), r2 = term.cell(15, 84);
      E.css(p1, { left: sx + r1.x * 0.93 - 40, top: sy + (r1.y + 34) * 0.93 });
      E.css(p2, { left: sx + r2.x * 0.93 - 40, top: sy + (r2.y + 34) * 0.93 });
      E.pop(p1, t, 3.2, 4.4);
      E.pop(p2, t, 4.9, 5.95);
    };
  },
});

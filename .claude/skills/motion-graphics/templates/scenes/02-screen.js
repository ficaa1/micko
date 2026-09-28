// A real screen: headline on the left, the terminal swings in on the right,
// then a highlight box and a pill call out one region. Find coordinates with
//   node lib/find.mjs VIDEO_NAME main "some text"
E.scene({
  id: 'screen', dur: 6,
  build(root) {
    const h = E.headline(root, { x: 110, y: 250, width: 560, kicker: 'the main view', title: 'One short\nclaim', body: 'One sentence of why it matters to the viewer.' });
    const term = E.term(root, 'main', { title: 'yourapp --demo' });
    const cam = { fx: term.w / 2, fy: term.h / 2, X: 1300, Y: 560, s: 0.93 };
    const box = E.hl(term, term.cell(2, 1, 6, 40));          // rows 2-6, columns 1-40
    const pill = E.pill(root, 'what this part shows', 'var(--red)');
    return (t) => {
      h.update(t, 0.3, 5.4);                                  // in at 0.3 s, out at 5.4 s
      const pin = E.p(t, 0, 1.0, 'expo'), pout = E.p(t, 5.4, 6, 'in');
      E.cam(term.win, { ...cam, X: E.lerp(2400, cam.X, pin), ry: (1 - pin) * -35, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      E.set(box, { o: E.inOut(t, 2.0, 5.0, 0.3) });
      const at = E.toStage(cam, term.cell(6, 1).x, term.cell(6, 1).y + 40);
      E.css(pill, { left: at.x, top: at.y });
      E.pop(pill, t, 2.2, 4.9);
    };
  },
});

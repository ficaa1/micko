// Nodes: the tree reads like the pipeline. Progress line, nested retries,
// dependencies, exit handler, then the node info panel.
E.scene({
  id: 'nodes', dur: 6.5,
  build(root) {
    const h = E.headline(root, { x: 1235, y: 250, width: 600, kicker: 'nodes tab, rebuilt', title: 'Reads like\nthe pipeline', body: 'Retries nest under their Retry node, tasks show what they wait for, and the exit handler gets its own tree.' });
    const term = E.term(root, 'nightly_nodes');
    term.layer('train_info');
    const cam = { fx: term.w / 2, fy: term.h / 2, X: 640, Y: 540, s: 0.93 };
    const H = [
      [E.hl(term, term.cell(3, 1, 3, 60), '#3f5bb8'), 'a progress line: bar, counts by state, time against the estimate', '#3f5bb8', 1.1, 2.2],
      [E.hl(term, term.cell(8, 2, 11, 60), '#d7263d'), 'retry attempts nest under their Retry node, with exit codes', '#d7263d', 2.3, 3.3],
      [E.hl(term, term.cell(12, 2, 13, 36), '#6f8a2a'), 'what each task waits for, and the exit handler as its own tree', '#6f8a2a', 3.4, 4.3],
      [E.hl(term, term.cell(15, 1, 26, 120), '#6f7fd6'), '<span class=g>i</span> opens everything the workflow says about a node', '#6f7fd6', 4.75, 6.0],
    ].map(([box, text, col, a, b]) => {
      const p = E.pill(root, text, col); p.style.fontSize = '26px';
      return { box, p, a, b };
    });
    const spot = E.spot(term, term.cell(3, 1, 3, 60));
    const cells = [term.cell(3, 1, 3, 60), term.cell(8, 2, 11, 60), term.cell(12, 2, 13, 36), term.cell(15, 1, 26, 120)];

    return (t) => {
      h.update(t, 0.3, 5.95);
      const pin = E.p(t, 0, 0.9, 'expo'), pout = E.p(t, 6.0, 6.5, 'in');
      E.cam(term.win, { ...cam, X: cam.X - (1 - pin) * 900 - pout * 200, ry: (1 - pin) * 30, persp: 2400, o: E.clamp(pin * 2) * (1 - pout) });
      term.wipe('nightly_nodes', 'train_info', E.p(t, 4.3, 4.75, 'inOut'));
      let best = 0, bi = 0;
      H.forEach(({ box, p, a, b }, i) => {
        const o = E.inOut(t, a, b, 0.3); E.set(box, { o });
        if (o > best) { best = o; bi = i; }
        E.css(p, { left: 108, top: 948 });
        E.pop(p, t, a + 0.1, b - 0.1);
      });
      spot.place(cells[bi]); spot.set(best * 0.85);
    };
  },
});

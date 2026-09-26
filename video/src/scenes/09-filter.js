// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'filter', dur: 5,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'filter' });
    return (t) => h.update(t, 0, 5 - 0.5);
  },
});

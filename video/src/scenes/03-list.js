// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'list', dur: 7,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'list' });
    return (t) => h.update(t, 0, 7 - 0.5);
  },
});

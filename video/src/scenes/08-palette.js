// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'palette', dur: 6,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'palette' });
    return (t) => h.update(t, 0, 6 - 0.5);
  },
});

// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'explain', dur: 7,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'explain' });
    return (t) => h.update(t, 0, 7 - 0.5);
  },
});

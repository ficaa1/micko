// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'actions', dur: 6,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'actions' });
    return (t) => h.update(t, 0, 6 - 0.5);
  },
});

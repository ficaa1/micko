// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'timeline', dur: 8,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'timeline' });
    return (t) => h.update(t, 0, 8 - 0.5);
  },
});

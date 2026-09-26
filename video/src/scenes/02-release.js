// TODO: placeholder until this scene is written (see PROGRESS.md).
E.scene({
  id: 'release', dur: 4.5,
  build(root) {
    const h = E.headline(root, { x: 160, y: 400, kicker: 'todo', title: 'release' });
    return (t) => h.update(t, 0, 4.5 - 0.5);
  },
});

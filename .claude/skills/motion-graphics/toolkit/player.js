// Preview player: open src/index.html in a browser. ?scene=ID&t=SEC shows one
// frame; ?render=1 is what render.mjs uses (no controls, 1:1 stage).
(function () {
  const q = new URLSearchParams(location.search);
  if (q.has('render')) return;
  document.body.classList.add('preview');
  const stage = document.getElementById('stage');
  const fit = () => { const k = Math.min(1, (innerWidth - 20) / 1920); stage.style.transform = `scale(${k})`; stage.style.marginBottom = `${1080 * (k - 1)}px`; stage.style.marginRight = `${1920 * (k - 1)}px`; };
  addEventListener('resize', fit); fit();
  const c = document.getElementById('controls');
  const total = E.total();
  c.innerHTML = `<button id=pp>play</button><select id=sc>${E.scenes.map((s) => `<option value=${s.id}>${s.id} (${s.dur}s)</option>`).join('')}</select><input id=sl type=range min=0 max=${total} step=0.0333 value=0> <span id=tt></span>`;
  let T = 0, playing = false, last = 0;
  const draw = () => { E.renderGlobal(T); document.getElementById('sl').value = T; document.getElementById('tt').textContent = `${T.toFixed(2)}s / ${total}s  ${E.at(T).s.id} @ ${E.at(T).t.toFixed(2)}`; };
  if (q.has('scene')) T = E.offset(q.get('scene')) + parseFloat(q.get('t') || '0');
  document.getElementById('sl').oninput = (e) => { T = +e.target.value; draw(); };
  document.getElementById('sc').onchange = (e) => { T = E.offset(e.target.value); draw(); };
  document.getElementById('pp').onclick = () => { playing = !playing; last = performance.now(); if (playing) requestAnimationFrame(tick); };
  function tick(now) { if (!playing) return; T = (T + (now - last) / 1000) % total; last = now; draw(); requestAnimationFrame(tick); }
  E.ready().then(draw);
})();

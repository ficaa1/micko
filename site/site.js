// Draws the captured screens in screens.js and runs the page's small animations.
(() => {
  const COLS = 124, CW = 9, LH = 20, PAD = 14;
  const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const esc = (s) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);

  // Block elements are drawn as cell backgrounds, so bars have no seams.
  const cut = (dir, pct) => `linear-gradient(${dir},currentColor ${pct}%,transparent ${pct}%)`;
  const shade = (pct) => `color-mix(in srgb,currentColor ${pct}%,transparent)`;
  const BLOCKS = { '█': 'currentColor', '▀': cut('180deg', 50), '▄': cut('0deg', 50), '▐': cut('270deg', 50), '▕': cut('270deg', 12.5),
    '▔': cut('180deg', 12.5), '▁': cut('0deg', 12.5), '░': shade(28), '▒': shade(50), '▓': shade(75) };
  ['▏', '▎', '▍', '▌', '▋', '▊', '▉'].forEach((ch, i) => (BLOCKS[ch] = cut('90deg', (i + 1) * 12.5)));

  const cache = {};
  const screenHTML = (name) => {
    if (cache[name]) return cache[name];
    const sc = SCREENS[name], sk = SKINS[sc.skin];
    let h = '';
    for (const row of sc.rows) {
      h += '<div class="r">';
      for (const [text, fg, bg, fl] of row) {
        let st = `color:${fg || sk.text};`;
        if (bg) st += `background:${bg};`;
        if (fl & 1) st += 'font-weight:700;';
        if (fl & 2) st += 'opacity:.6;';
        if (fl & 8) st += 'text-decoration:underline;';
        let inner = '';
        for (const ch of text) {
          const c = ch.codePointAt(0);
          if (c < 128) inner += esc(ch);
          else if (BLOCKS[ch]) inner += `<i class="${'░▒▓'.includes(ch) ? 'k s' : 'k'}" style="background:${BLOCKS[ch]}"></i>`;
          else if (c >= 0x2500 && c <= 0x257f) inner += ch;
          else inner += `<i>${ch}</i>`;
        }
        h += `<span${bg ? ' class="b"' : ''} style="${st}">${inner}</span>`;
      }
      h += '</div>';
    }
    return (cache[name] = h);
  };

  // Each .screen holds one terminal at its natural size, scaled to the box.
  const fit = new ResizeObserver((entries) => {
    for (const { target } of entries) {
      const term = target.firstChild, rows = +target.dataset.rows || 32;
      const s = target.clientWidth / (COLS * CW + PAD * 2);
      term.style.transform = `scale(${s})`;
      target.style.height = (rows * LH + PAD * 2) * s + 'px';
    }
  });
  const show = (box, name) => {
    let term = box.firstChild;
    if (!term) { term = document.createElement('div'); term.className = 'term'; box.append(term); fit.observe(box); }
    term.innerHTML = screenHTML(name);
    const bg = SKINS[SCREENS[name].skin].bg;
    term.style.background = bg;
    box.closest('.win').style.background = bg;
    box.dataset.screen = name;
  };
  document.querySelectorAll('.screen[data-screen]').forEach((b) => show(b, b.dataset.screen));

  // Runs fn(true) when el scrolls into view and fn(false) when it leaves.
  const watch = (el, fn, threshold = 0.25) =>
    new IntersectionObserver(([e]) => fn(e.isIntersecting), { threshold }).observe(el);

  document.querySelectorAll('.in').forEach((el) => watch(el, (on) => on && el.classList.add('seen'), 0.15));

  // The filter, typed one key at a time, replaying the captured screens.
  const qBox = document.querySelector('#filter .screen'), qLine = document.querySelector('#filter .query .t');
  const QUERY = 'phase=Failed age<3h';
  const paint = (s) => s.replace(/(\w+)([=<>!]+)(\S*)/g, '<span class="k">$1</span><span class="op">$2</span><span class="v">$3</span>');
  let qTimer = null;
  const typeQuery = (i = 0) => {
    show(qBox, 'filter_' + String(i).padStart(2, '0'));
    qLine.innerHTML = paint(esc(QUERY.slice(0, i)));
    qTimer = setTimeout(() => typeQuery(i < QUERY.length ? i + 1 : 0), i === 0 ? 900 : i < QUERY.length ? 130 + Math.random() * 90 : 3200);
  };
  if (still) { show(qBox, 'filter_19'); qLine.innerHTML = paint(esc(QUERY)); }
  else watch(qBox, (on) => { clearTimeout(qTimer); if (on) typeQuery(); });

  // Skins: pick one, or let them cycle until you do.
  const sBox = document.querySelector('#skins .screen'), sBtns = [...document.querySelectorAll('#skins button')];
  let sTimer = null, sAt = sBtns.findIndex((b) => b.dataset.skin === 'monokai');
  const pick = (i) => {
    sAt = i; show(sBox, 'skin_' + sBtns[i].dataset.skin);
    sBtns.forEach((b, j) => b.setAttribute('aria-pressed', j === i));
  };
  sBtns.forEach((b, i) => {
    b.style.setProperty('--sw', SKINS[b.dataset.skin].bg);
    b.addEventListener('click', () => { clearInterval(sTimer); sTimer = 'stopped'; pick(i); });
  });
  pick(sAt);
  if (!still) watch(sBox, (on) => {
    if (sTimer === 'stopped') return;
    clearInterval(sTimer);
    if (on) sTimer = setInterval(() => pick((sAt + 1) % sBtns.length), 1800);
  });

  // Mićko on his perch, replayed from the captured frames and their timing.
  const pBox = document.querySelector('#micko .screen');
  const seq = SEQS.perch, loop = 40;
  let pT0 = 0, pRaf = 0, pLast = '';
  const perch = (now) => {
    const t = ((now - pT0) / 1000) % loop;
    let f = seq[0][1];
    for (const [at, name] of seq) if (at <= t) f = name;
    if (f !== pLast) { show(pBox, f); pLast = f; }
    pRaf = requestAnimationFrame(perch);
  };
  show(pBox, seq[0][1]);
  if (!still) watch(pBox, (on) => { cancelAnimationFrame(pRaf); if (on) { pT0 = performance.now(); pRaf = requestAnimationFrame(perch); } });

  // The tour plays muted while it is on screen; sound is one click away.
  const film = document.querySelector('.film'), video = film.querySelector('video');
  const playBtn = film.querySelector('[data-play]'), soundBtn = film.querySelector('[data-sound]');
  let paused = still;
  const sync = () => {
    playBtn.textContent = video.paused ? '▶ play' : '❚❚ pause';
    soundBtn.textContent = video.muted ? 'sound off' : 'sound on';
  };
  watch(film, (on) => { if (on && !paused) video.play().catch(() => {}); else video.pause(); }, 0.3);
  playBtn.addEventListener('click', () => { paused = !video.paused; paused ? video.pause() : video.play(); });
  soundBtn.addEventListener('click', () => { video.muted = !video.muted; if (!video.muted && video.paused) { paused = false; video.play(); } sync(); });
  ['play', 'pause', 'volumechange'].forEach((e) => video.addEventListener(e, sync));
  sync();

  document.querySelectorAll('.copy').forEach((b) => b.addEventListener('click', () => {
    navigator.clipboard.writeText(b.dataset.copy).then(() => {
      b.textContent = 'copied'; setTimeout(() => (b.textContent = 'copy'), 1400);
    });
  }));
})();

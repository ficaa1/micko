// Renders one video (videos/VIDEO/index.html). Usage, from video/:
//   node lib/render.mjs VIDEO                  render stale scenes, then join + encode
//   node lib/render.mjs VIDEO intro list       only these scenes (then join if all exist)
//   node lib/render.mjs VIDEO --force          ignore the up-to-date check
//   node lib/render.mjs VIDEO --jobs 3         scenes rendered in parallel (default 3)
//   node lib/render.mjs VIDEO --still list:2.5 [more...]   out/VIDEO/stills/list-2.5.png only
//   node lib/render.mjs VIDEO --sheet list     contact sheet of a scene (every 0.5 s)
//   node lib/render.mjs VIDEO --music song.mp3 your own track (or drop videos/VIDEO/music.mp3)
//   node lib/render.mjs VIDEO --out take2.mp4  final file (default: <meta name=output> in
//                                              index.html, else renders/VIDEO.mp4)
// A scene is up to date when out/VIDEO/scenes/<id>.mp4 is newer than its file,
// the video's index.html and screens.js, and lib/engine.js + lib/style.css, so an
// interrupted run resumes cheaply. Without music, the synthesized soundtrack
// (lib/soundtrack.mjs) is rebuilt whenever the scenes changed.
import fs from 'node:fs';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { chromium } from 'playwright';
import ffmpeg from 'ffmpeg-static';

const lib = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(lib, '..');
const FPS = 30;
const args = process.argv.slice(2);
const flag = (f) => args.includes(f);
const opt = (f, d) => { const i = args.indexOf(f); return i >= 0 ? args[i + 1] : d; };
const jobs = +opt('--jobs', 3);
const force = flag('--force');
const stills = []; const sheets = []; const pos = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--still') { while (args[i + 1] && !args[i + 1].startsWith('--')) stills.push(args[++i]); }
  else if (args[i] === '--sheet') { while (args[i + 1] && !args[i + 1].startsWith('--')) sheets.push(args[++i]); }
  else if (['--jobs', '--music', '--out'].includes(args[i])) i++;
  else if (!args[i].startsWith('--')) pos.push(args[i]);
}
const video = pos.shift();
const vdir = video && path.join(root, 'videos', video);
if (!video || !fs.existsSync(path.join(vdir, 'index.html'))) {
  const have = fs.readdirSync(path.join(root, 'videos')).join(', ');
  console.error(`usage: node lib/render.mjs VIDEO [scene...] [options]   (videos: ${have})`);
  process.exit(1);
}
const only = pos;
const out = path.join(root, 'out', video);
fs.mkdirSync(path.join(out, 'scenes'), { recursive: true });

const pageURL = pathToFileURL(path.join(vdir, 'index.html')).href + '?render=1';
const browser = await chromium.launch();
async function newPage() {
  const page = await browser.newPage({ viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 });
  page.on('pageerror', (e) => console.error('page error:', e.message));
  await page.goto(pageURL);
  await page.evaluate(() => E.ready());
  return page;
}
const frame = (page, id, t) => page.evaluate(([id, t]) => E.renderAt(id, t), [id, t]).then(() => page.screenshot({ type: 'png' }));

const page0 = await newPage();
const scenes = await page0.evaluate(() => E.scenes.map((s) => ({ id: s.id, dur: s.dur })));
const html = fs.readFileSync(path.join(vdir, 'index.html'), 'utf8');
const fileOf = (id) => { const m = html.match(new RegExp(`scenes/(\\d+-${id}\\.js)`)); return m && path.join(vdir, 'scenes', m[1]); };
const rel = (f) => path.relative(root, f);

function run(a, input) {
  return new Promise((res, rej) => {
    const p = spawn(ffmpeg, a, { stdio: [input ? 'pipe' : 'ignore', 'inherit', 'inherit'] });
    p.on('exit', (c) => (c === 0 ? res() : rej(new Error('ffmpeg exit ' + c))));
    if (input) input(p.stdin);
  });
}

if (stills.length || sheets.length) {
  const sdir = path.join(out, 'stills');
  fs.mkdirSync(sdir, { recursive: true });
  for (const s of stills) {
    const [id, t] = s.split(':');
    const f = path.join(sdir, `${id}-${t}.png`);
    fs.writeFileSync(f, await frame(page0, id, +t));
    console.log('wrote', rel(f));
  }
  for (const id of sheets) {
    const sc = scenes.find((s) => s.id === id);
    const tmp = path.join(sdir, `sheet-${id}`); fs.mkdirSync(tmp, { recursive: true });
    let n = 0;
    for (let t = 0.25; t < sc.dur; t += 0.5) fs.writeFileSync(path.join(tmp, `${String(n++).padStart(3, '0')}.png`), await frame(page0, id, t));
    const f = path.join(sdir, `sheet-${id}.png`);
    await run(['-y', '-loglevel', 'error', '-i', path.join(tmp, '%03d.png'), '-vf', `scale=480:-1,tile=4x${Math.ceil(n / 4)}:padding=4`, '-frames:v', '1', f]);
    console.log('wrote', rel(f));
  }
  await browser.close();
  process.exit(0);
}

const mtime = (f) => (f && fs.existsSync(f) ? fs.statSync(f).mtimeMs : 0);
const shared = [path.join(lib, 'engine.js'), path.join(lib, 'style.css'), path.join(vdir, 'screens.js'), path.join(vdir, 'index.html')];
const stale = (id) => force || mtime(path.join(out, 'scenes', `${id}.mp4`)) < Math.max(mtime(fileOf(id)), ...shared.map(mtime));

const todo = scenes.filter((s) => (only.length ? only.includes(s.id) : true) && stale(s.id));
console.log(todo.length ? `rendering ${video}: ${todo.map((s) => s.id).join(', ')}` : `${video}: all scenes up to date`);
async function renderScene(page, s) {
  const n = Math.round(s.dur * FPS);
  const dst = path.join(out, 'scenes', `${s.id}.mp4`), tmp = dst + '.part.mp4';
  const t0 = Date.now();
  await run(['-y', '-loglevel', 'error', '-f', 'image2pipe', '-framerate', String(FPS), '-c:v', 'png', '-i', '-',
    '-c:v', 'libx264', '-preset', 'slow', '-crf', '16', '-pix_fmt', 'yuv420p', '-r', String(FPS), tmp], async (stdin) => {
    for (let i = 0; i < n; i++) {
      const buf = await frame(page, s.id, i / FPS);
      if (!stdin.write(buf)) await new Promise((r) => stdin.once('drain', r));
    }
    stdin.end();
  });
  fs.renameSync(tmp, dst);
  console.log(`  ${s.id}: ${n} frames in ${((Date.now() - t0) / 1000).toFixed(0)}s`);
}
const queue = [...todo];
await Promise.all(Array.from({ length: Math.min(jobs, queue.length) }, async (_, k) => {
  const page = k === 0 ? page0 : await newPage();
  while (queue.length) await renderScene(page, queue.shift());
}));
await browser.close();

const missing = scenes.filter((s) => !fs.existsSync(path.join(out, 'scenes', `${s.id}.mp4`)));
if (missing.length) { console.log(`not joined yet; missing: ${missing.map((s) => s.id).join(', ')}`); process.exit(0); }
const list = path.join(out, 'concat.txt');
fs.writeFileSync(list, scenes.map((s) => `file 'scenes/${s.id}.mp4'`).join('\n') + '\n');

// Join losslessly into out/VIDEO/master.mp4 with a soundtrack, then encode the
// smaller final video from it. Music, first found wins: --music FILE, then
// videos/VIDEO/music.{mp3,m4a,wav,flac,ogg} (trimmed to the video with a 2 s
// fade-out), then the synthesized out/VIDEO/soundtrack.m4a.
const master = path.join(out, 'master.mp4');
const outName = opt('--out') || (html.match(/<meta name="output" content="([^"]+)"/) || [])[1] || `${video}.mp4`;
const final = path.isAbsolute(outName) || outName.includes('/') ? path.resolve(root, outName) : path.join(root, 'renders', outName);
fs.mkdirSync(path.dirname(final), { recursive: true });
const total = scenes.reduce((a, s) => a + s.dur, 0);
const music = opt('--music') || ['mp3', 'm4a', 'wav', 'flac', 'ogg'].map((e) => path.join(vdir, 'music.' + e)).find((f) => fs.existsSync(f));
const synth = path.join(out, 'soundtrack.m4a');
if (!music && mtime(synth) < Math.max(...scenes.map((s) => mtime(fileOf(s.id))), mtime(path.join(vdir, 'index.html')), mtime(path.join(lib, 'soundtrack.mjs')))) {
  spawnSync(process.execPath, [path.join(lib, 'soundtrack.mjs'), video], { stdio: 'inherit' });
}
const joinArgs = ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', list];
if (music) {
  joinArgs.push('-i', music, '-map', '0:v', '-map', '1:a', '-af', `afade=t=out:st=${Math.max(0, total - 2)}:d=2`,
    '-t', String(total), '-c:a', 'aac', '-b:a', '256k');
  console.log('music:', path.relative(root, path.resolve(music)));
} else if (fs.existsSync(synth)) joinArgs.push('-i', synth, '-map', '0:v', '-map', '1:a', '-shortest', '-c:a', 'copy');
joinArgs.push('-c:v', 'copy', '-movflags', '+faststart', master);
await run(joinArgs);
await run(['-y', '-loglevel', 'error', '-i', master, '-c:v', 'libx264', '-preset', 'slow', '-crf', '22', '-tune', 'animation',
  '-pix_fmt', 'yuv420p', '-c:a', 'copy', '-movflags', '+faststart', final]);
console.log('wrote', rel(final), (fs.statSync(final).size / 1e6).toFixed(1) + ' MB');

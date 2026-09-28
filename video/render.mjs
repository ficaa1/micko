// Renders the video. Usage:
//   node render.mjs                  render stale scenes, then join them
//   node render.mjs intro list       only these scenes (then join)
//   node render.mjs --force          ignore the up-to-date check
//   node render.mjs --jobs 3         scenes rendered in parallel (default 3)
//   node render.mjs --still list:2.5 [more...]   write out/stills/list-2.5.png only
//   node render.mjs --sheet list     contact sheet of a scene (every 0.5 s)
//   node render.mjs --music song.mp3 use your own track (or drop audio/music.mp3)
//   node render.mjs --out take2.mp4  name of the final video (default micko-0.6.mp4)
// A scene is up to date when out/scenes/<id>.mp4 is newer than its source
// file and the shared engine files, so an interrupted run resumes cheaply.
import fs from 'node:fs';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { chromium } from 'playwright';
import ffmpeg from 'ffmpeg-static';

const dir = path.dirname(fileURLToPath(import.meta.url));
const src = path.join(dir, 'src');
const out = path.join(dir, 'out');
const FPS = 30;
const args = process.argv.slice(2);
const flag = (f) => args.includes(f);
const opt = (f, d) => { const i = args.indexOf(f); return i >= 0 ? args[i + 1] : d; };
const jobs = +opt('--jobs', 3);
const force = flag('--force');
const stills = []; const sheets = []; const only = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--still') { while (args[i + 1] && !args[i + 1].startsWith('--')) stills.push(args[++i]); }
  else if (args[i] === '--sheet') { while (args[i + 1] && !args[i + 1].startsWith('--')) sheets.push(args[++i]); }
  else if (['--jobs', '--music', '--out'].includes(args[i])) i++;
  else if (!args[i].startsWith('--')) only.push(args[i]);
}
fs.mkdirSync(path.join(out, 'scenes'), { recursive: true });

const pageURL = pathToFileURL(path.join(src, 'index.html')).href + '?render=1';
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
const html = fs.readFileSync(path.join(src, 'index.html'), 'utf8');
const fileOf = (id) => { const m = html.match(new RegExp(`scenes/(\\d+-${id}\\.js)`)); return m && path.join(src, 'scenes', m[1]); };

if (stills.length || sheets.length) {
  fs.mkdirSync(path.join(out, 'stills'), { recursive: true });
  for (const s of stills) {
    const [id, t] = s.split(':');
    const f = path.join(out, 'stills', `${id}-${t}.png`);
    fs.writeFileSync(f, await frame(page0, id, +t));
    console.log('wrote', path.relative(dir, f));
  }
  for (const id of sheets) {
    const sc = scenes.find((s) => s.id === id);
    const tmp = path.join(out, 'stills', `sheet-${id}`); fs.mkdirSync(tmp, { recursive: true });
    let n = 0;
    for (let t = 0.25; t < sc.dur; t += 0.5) fs.writeFileSync(path.join(tmp, `${String(n++).padStart(3, '0')}.png`), await frame(page0, id, t));
    const f = path.join(out, 'stills', `sheet-${id}.png`);
    await run(['-y', '-loglevel', 'error', '-i', path.join(tmp, '%03d.png'), '-vf', `scale=480:-1,tile=4x${Math.ceil(n / 4)}:padding=4`, '-frames:v', '1', f]);
    console.log('wrote', path.relative(dir, f));
  }
  await browser.close();
  process.exit(0);
}

function run(a, input) {
  return new Promise((res, rej) => {
    const p = spawn(ffmpeg, a, { stdio: [input ? 'pipe' : 'ignore', 'inherit', 'inherit'] });
    p.on('exit', (c) => (c === 0 ? res() : rej(new Error('ffmpeg exit ' + c))));
    if (input) input(p.stdin);
  });
}
const mtime = (f) => (fs.existsSync(f) ? fs.statSync(f).mtimeMs : 0);
const shared = ['engine.js', 'style.css', 'screens.js', 'index.html'].map((f) => path.join(src, f));
const stale = (id) => {
  const mp4 = path.join(out, 'scenes', `${id}.mp4`);
  return force || mtime(mp4) < Math.max(mtime(fileOf(id)), ...shared.map(mtime));
};

const todo = scenes.filter((s) => (only.length ? only.includes(s.id) : true) && stale(s.id));
console.log(todo.length ? `rendering: ${todo.map((s) => s.id).join(', ')}` : 'all scenes up to date');
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
// Join losslessly into out/master.mp4 with a soundtrack, then encode the
// smaller final video from it (--out NAME.mp4, default micko-0.6.mp4).
// Music, first found wins: --music FILE, then audio/music.{mp3,m4a,wav,flac,ogg}
// (your own track: trimmed to the video with a 2 s fade-out), then the
// synthesized audio/soundtrack.m4a.
const master = path.join(out, 'master.mp4');
const final = path.resolve(dir, opt('--out', 'micko-0.6.mp4'));
const total = scenes.reduce((a, s) => a + s.dur, 0);
const music = opt('--music') || ['mp3', 'm4a', 'wav', 'flac', 'ogg'].map((e) => path.join(dir, 'audio', 'music.' + e)).find((f) => fs.existsSync(f));
const synth = path.join(dir, 'audio', 'soundtrack.m4a');
const joinArgs = ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', list];
if (music) {
  joinArgs.push('-i', music, '-map', '0:v', '-map', '1:a', '-af', `afade=t=out:st=${Math.max(0, total - 2)}:d=2`,
    '-t', String(total), '-c:a', 'aac', '-b:a', '256k');
  console.log('music:', path.relative(dir, music));
} else if (fs.existsSync(synth)) joinArgs.push('-i', synth, '-map', '0:v', '-map', '1:a', '-shortest', '-c:a', 'copy');
joinArgs.push('-c:v', 'copy', '-movflags', '+faststart', master);
await run(joinArgs);
await run(['-y', '-loglevel', 'error', '-i', master, '-c:v', 'libx264', '-preset', 'slow', '-crf', '22', '-tune', 'animation',
  '-pix_fmt', 'yuv420p', '-c:a', 'copy', '-movflags', '+faststart', final]);
console.log('wrote', path.relative(dir, final), (fs.statSync(final).size / 1e6).toFixed(1) + ' MB');

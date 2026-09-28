// Synthesizes a placeholder soundtrack for one video, timed from its scenes:
//   node lib/soundtrack.mjs VIDEO   ->  out/VIDEO/soundtrack.m4a
// A pad progression throughout, a bass pulse and a light beat that come in
// during the video, a whoosh into every cut, and the cues each scene lists in
// its `sfx`: [[seconds, 'click' | 'thump' | 'impact'], ...]. No samples, so
// there is nothing to license. render.mjs calls this when the scenes change.
//
// index.html may set where the layers start, by scene id (defaults: the 2nd,
// 3rd and 4th scenes):
//   <meta name="bass-from" content="release"> <meta name="beat-from" content="list">
//   <meta name="hats-from" content="timeline">
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import ffmpeg from 'ffmpeg-static';

const lib = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(lib, '..');
const video = process.argv[2];
if (!video) { console.error('usage: node lib/soundtrack.mjs VIDEO'); process.exit(1); }
const vdir = path.join(root, 'videos', video);
const html = fs.readFileSync(path.join(vdir, 'index.html'), 'utf8');
// Read each scene's id, dur and sfx by running its file against a stub E.
const scenes = [];
const E = { scene: (s) => scenes.push({ id: s.id, dur: s.dur, sfx: s.sfx || [] }) };
E.typing = (a, n, per) => Array.from({ length: n }, (_, k) => [a + (k + 1) * per, 'click']);
for (const m of html.matchAll(/<script src="(scenes\/[^"]+\.js)"/g)) vm.runInNewContext(fs.readFileSync(path.join(vdir, m[1]), 'utf8'), { E });
const at = {}; let T = 0;
for (const s of scenes) { at[s.id] = T; T += s.dur; }
const meta = (n, i) => { const v = (html.match(new RegExp(`<meta name="${n}" content="([^"]+)"`)) || [])[1]; return v in at ? at[v] : at[scenes[Math.min(i, scenes.length - 1)].id]; };
const bassFrom = meta('bass-from', 1), beatFrom = meta('beat-from', 2), hatsFrom = meta('hats-from', 3);
const lastStart = at[scenes[scenes.length - 1].id];

const SR = 48000, N = Math.ceil(T * SR);
const L = new Float32Array(N), R = new Float32Array(N);
const add = (i, l, r = l) => { if (i >= 0 && i < N) { L[i] += l; R[i] += r; } };
const hz = (m) => 440 * Math.pow(2, (m - 69) / 12);
let seed = 7; const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647) * 2 - 1;

// Pad: Am F C G, one bar each at 100 bpm, additive saw-ish voices.
const BEAT = 0.6, BAR = BEAT * 4;
const chords = [[57, 60, 64, 69], [53, 57, 60, 65], [48, 55, 60, 64], [55, 59, 62, 67]];
for (let b = 0; b * BAR < T; b++) {
  const t0 = b * BAR, ch = chords[b % 4];
  const len = BAR + 1.2;
  for (const [vi, m] of ch.entries()) {
    for (let k = 0; k < len * SR; k++) {
      const t = k / SR, i = Math.floor((t0 + t) * SR);
      const env = Math.min(1, t / 0.5) * Math.min(1, Math.max(0, (len - t) / 1.2));
      let v = 0;
      for (let hn = 1; hn <= 5; hn++) v += Math.sin(2 * Math.PI * hz(m) * hn * t * (1 + (vi % 2 ? 0.0015 : -0.0015))) / (hn * hn);
      const w = Math.sin(2 * Math.PI * hz(m) * t * 1.004);
      add(i, v * env * 0.03, (v * 0.6 + w * 0.4) * env * 0.03);
    }
  }
  // Bass: root, pulsing eighths.
  for (let e = 0; e < 8; e++) {
    const s0 = t0 + e * BEAT / 2; if (s0 < bassFrom || s0 > lastStart + 5) continue;
    for (let k = 0; k < 0.28 * SR; k++) {
      const t = k / SR, env = Math.exp(-t * 9) * Math.min(1, t / 0.005);
      add(Math.floor((s0 + t) * SR), Math.tanh(Math.sin(2 * Math.PI * hz(ch[0] - 24) * t) * 1.6) * env * 0.09);
    }
  }
}
// Kick on the beat, hats off the beat.
for (let t0 = Math.ceil(beatFrom / BEAT) * BEAT; t0 < lastStart + 5.5; t0 += BEAT) {
  for (let k = 0; k < 0.3 * SR; k++) {
    const t = k / SR, f = 45 + 80 * Math.exp(-t * 30);
    add(Math.floor((t0 + t) * SR), Math.sin(2 * Math.PI * f * t) * Math.exp(-t * 11) * 0.16);
  }
  if (t0 > hatsFrom) {
    let hp = 0, prev = 0;
    for (let k = 0; k < 0.04 * SR; k++) {
      const n = rnd(); hp = 0.8 * (hp + n - prev); prev = n;
      const t = k / SR; add(Math.floor((t0 + BEAT / 2 + t) * SR), hp * Math.exp(-t * 90) * 0.035, hp * Math.exp(-t * 90) * 0.028);
    }
  }
}
// Whoosh into every cut: noise through a rising low-pass.
for (const s of scenes.slice(1)) {
  const a = at[s.id] - 0.55, d = 0.8; let lp = 0;
  for (let k = 0; k < d * SR; k++) {
    const t = k / SR, x = t / d, cut = 0.02 + 0.3 * Math.sin(Math.PI * x);
    lp += cut * (rnd() - lp);
    const env = Math.sin(Math.PI * Math.min(1, x * 1.2)) ** 2;
    add(Math.floor((a + t) * SR), lp * env * 0.12 * (1 - x * 0.3), lp * env * 0.12 * (0.7 + x * 0.3));
  }
}
// The scenes' own cues.
const cue = {
  click(c) {
    const f = 1800 + rnd() * 400;
    for (let k = 0; k < 0.025 * SR; k++) { const t = k / SR; const v = (Math.sin(2 * Math.PI * f * t) * 0.5 + rnd() * 0.5) * Math.exp(-t * 260) * 0.08; add(Math.floor((c + t) * SR), v, v * 0.8); }
  },
  thump(c) {
    for (let k = 0; k < 0.35 * SR; k++) { const t = k / SR; add(Math.floor((c + t) * SR), Math.sin(2 * Math.PI * (90 + 60 * Math.exp(-t * 20)) * t) * Math.exp(-t * 12) * 0.12); }
  },
  impact(c) {
    for (let k = 0; k < 1.6 * SR; k++) { const t = k / SR; add(Math.floor((c + t) * SR), Math.sin(2 * Math.PI * (48 + 30 * Math.exp(-t * 8)) * t) * Math.exp(-t * 2.6) * 0.3 + rnd() * Math.exp(-t * 25) * 0.05); }
  },
};
let cues = 0;
for (const s of scenes) for (const [t, kind] of s.sfx) { if (cue[kind]) { cue[kind](at[s.id] + t); cues++; } }
// Master: fades and a soft clip.
const pcm = Buffer.alloc(N * 4);
for (let i = 0; i < N; i++) {
  const t = i / SR, g = Math.min(1, t / 0.6) * Math.min(1, Math.max(0, (T - t) / 1.4));
  pcm.writeInt16LE(Math.round(Math.tanh(L[i] * g * 1.4) * 32000), i * 4);
  pcm.writeInt16LE(Math.round(Math.tanh(R[i] * g * 1.4) * 32000), i * 4 + 2);
}
const hdr = Buffer.alloc(44);
hdr.write('RIFF', 0); hdr.writeUInt32LE(36 + pcm.length, 4); hdr.write('WAVE', 8); hdr.write('fmt ', 12);
hdr.writeUInt32LE(16, 16); hdr.writeUInt16LE(1, 20); hdr.writeUInt16LE(2, 22); hdr.writeUInt32LE(SR, 24);
hdr.writeUInt32LE(SR * 4, 28); hdr.writeUInt16LE(4, 32); hdr.writeUInt16LE(16, 34); hdr.write('data', 36); hdr.writeUInt32LE(pcm.length, 40);
const out = path.join(root, 'out', video);
fs.mkdirSync(out, { recursive: true });
const wav = path.join(out, 'soundtrack.wav');
fs.writeFileSync(wav, Buffer.concat([hdr, pcm]));
const m4a = path.join(out, 'soundtrack.m4a');
const r = spawnSync(ffmpeg, ['-y', '-loglevel', 'error', '-i', wav, '-af', 'loudnorm=I=-16:TP=-1.5:LRA=11', '-ar', '48000', '-c:a', 'aac', '-b:a', '192k', m4a], { stdio: 'inherit' });
if (r.status !== 0) process.exit(1);
console.log(`wrote ${path.relative(root, m4a)} (${T.toFixed(1)} s, ${scenes.length} scenes, ${cues} cues)`);

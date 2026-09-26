// Synthesizes the soundtrack to audio/soundtrack.m4a, timed from the scene
// durations in src/scenes/*.js: a pad progression, a light beat, a whoosh
// into every cut, an impact on the version reveal and key clicks under the
// typing. No samples, so there is nothing to license.
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import ffmpeg from 'ffmpeg-static';

const dir = path.dirname(fileURLToPath(import.meta.url));
const html = fs.readFileSync(path.join(dir, '../src/index.html'), 'utf8');
const scenes = [...html.matchAll(/scenes\/(\d+-([a-z]+)\.js)/g)].map((m) => {
  const src = fs.readFileSync(path.join(dir, '../src/scenes', m[1]), 'utf8');
  return { id: m[2], dur: +src.match(/dur:\s*([\d.]+)/)[1] };
});
const at = {}; let T = 0;
for (const s of scenes) { at[s.id] = T; T += s.dur; }
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
  // Bass from the release card on: root, pulsing eighths.
  if (t0 + BAR > at.release) for (let e = 0; e < 8; e++) {
    const s0 = t0 + e * BEAT / 2; if (s0 < at.release || s0 > at.outro + 5) continue;
    for (let k = 0; k < 0.28 * SR; k++) {
      const t = k / SR, env = Math.exp(-t * 9) * Math.min(1, t / 0.005);
      const v = Math.tanh(Math.sin(2 * Math.PI * hz(ch[0] - 24) * t) * 1.6) * env * 0.09;
      add(Math.floor((s0 + t) * SR), v);
    }
  }
}
// Kick on the beat, hats off the beat, from the list scene to the outro.
for (let t0 = at.list; t0 < at.outro + 5.5; t0 += BEAT) {
  for (let k = 0; k < 0.3 * SR; k++) {
    const t = k / SR, f = 45 + 80 * Math.exp(-t * 30);
    add(Math.floor((t0 + t) * SR), Math.sin(2 * Math.PI * f * t) * Math.exp(-t * 11) * 0.16);
  }
  if (t0 > at.timeline) {
    let hp = 0, prev = 0;
    for (let k = 0; k < 0.04 * SR; k++) {
      const n = rnd(); hp = 0.8 * (hp + n - prev); prev = n;
      const t = k / SR; add(Math.floor((t0 + BEAT / 2 + t) * SR), hp * Math.exp(-t * 90) * 0.035, hp * Math.exp(-t * 90) * 0.028);
    }
  }
}
// Whoosh into every cut: noise through a rising low-pass.
for (const s of scenes.slice(1)) {
  const c = at[s.id], a = c - 0.55, d = 0.8; let lp = 0;
  for (let k = 0; k < d * SR; k++) {
    const t = k / SR, x = t / d, cut = 0.02 + 0.3 * Math.sin(Math.PI * x);
    lp += cut * (rnd() - lp);
    const env = Math.sin(Math.PI * Math.min(1, x * 1.2)) ** 2;
    add(Math.floor((a + t) * SR), lp * env * 0.12 * (1 - x * 0.3), lp * env * 0.12 * (0.7 + x * 0.3));
  }
}
// Impact as 0.5 lands.
{ const c = at.release + 0.35;
  for (let k = 0; k < 1.6 * SR; k++) { const t = k / SR; add(Math.floor((c + t) * SR), Math.sin(2 * Math.PI * (48 + 30 * Math.exp(-t * 8)) * t) * Math.exp(-t * 2.6) * 0.3 + rnd() * Math.exp(-t * 25) * 0.05); } }
// Key clicks under the typing.
const clicks = [];
for (let k = 0; k < 17; k++) clicks.push(at.intro + 0.35 + (k + 1) * 1.1 / 17);
for (const x of [1.0, 1.4, 1.65, 1.9, 2.15, 2.85, 3.9]) clicks.push(at.palette + x);
for (let k = 0; k < 19; k++) clicks.push(at.filter + 0.9 + k * 0.095);
for (let k = 0; k < 3; k++) clicks.push(at.actions + 1.0 + k * 0.35);
for (let k = 0; k < 50; k++) clicks.push(at.outro + 1.5 + k / 38);
for (let k = 0; k < 15; k++) clicks.push(at.outro + 3.0 + k / 28);
for (const c of clicks) {
  const f = 1800 + rnd() * 400;
  for (let k = 0; k < 0.025 * SR; k++) { const t = k / SR; const v = (Math.sin(2 * Math.PI * f * t) * 0.5 + rnd() * 0.5) * Math.exp(-t * 260) * 0.08; add(Math.floor((c + t) * SR), v, v * 0.8); }
}
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
const wav = path.join(dir, '../out/soundtrack.wav');
fs.mkdirSync(path.dirname(wav), { recursive: true });
fs.writeFileSync(wav, Buffer.concat([hdr, pcm]));
const out = path.join(dir, 'soundtrack.m4a');
const r = spawnSync(ffmpeg, ['-y', '-loglevel', 'error', '-i', wav, '-af', 'loudnorm=I=-16:TP=-1.5:LRA=11', '-ar', '48000', '-c:a', 'aac', '-b:a', '192k', out], { stdio: 'inherit' });
if (r.status !== 0) process.exit(1);
console.log(`wrote audio/soundtrack.m4a (${T.toFixed(1)} s, ${scenes.length} scenes)`);

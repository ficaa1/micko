// Prints row/col of text in a captured screen, for placing highlights:
//   node lib/find.mjs VIDEO SCREEN "some text" "more text"
//   node lib/find.mjs VIDEO SCREEN --rows      (numbered plain rows)
import fs from 'node:fs';
const [video, name, ...needles] = process.argv.slice(2);
const plain = fs.readFileSync(new URL(`../videos/${video}/screens/${name}.ans`, import.meta.url), 'utf8')
  .replace(/\x1b\[[0-9;]*m/g, '').split('\n');
if (needles[0] === '--rows') plain.forEach((r, i) => console.log(String(i).padStart(2), r));
else for (const n of needles) plain.forEach((r, i) => { const c = [...r].join('').indexOf(n); if (c >= 0) console.log(`${JSON.stringify(n)} row ${i} col ${[...r.slice(0, c)].length}-${[...r.slice(0, c)].length + [...n].length - 1}`); });

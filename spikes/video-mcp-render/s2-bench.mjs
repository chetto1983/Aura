// S2 — one render of the parity project inside a CPU- and memory-limited container, with the
// container's own cgroup v2 counters sampled every 500 ms (https://docs.kernel.org/admin-guide/cgroup-v2.html:
// cpu.stat usage_usec / nr_throttled, memory.current, memory.peak). One render per container, so
// memory.peak is this render's peak. Usage: node s2-bench.mjs <a|a-nocapture|b|ffmpeg> <browser> <project.json> <label>
import { readFileSync } from 'node:fs';
import { analyseFilm, summary } from './lib/analyse.mjs';
import { browserPaths } from './lib/browsers.mjs';
import { writeJson } from './lib/measure.mjs';
import { compileInPage, renderPage, renderServer } from './lib/render.mjs';
import { ROOT, startServer } from './lib/server.mjs';

const [pipeline, name, projectFile, label] = process.argv.slice(2);
const path = browserPaths()[name];
const CG = '/sys/fs/cgroup';
const read = (file) => readFileSync(`${CG}/${file}`, 'utf8').trim();
const cpuStat = () => Object.fromEntries(read('cpu.stat').split('\n').map((l) => l.split(' ')).map(([k, v]) => [k, Number(v)]));

const samples = [];
let last = { t: performance.now(), usage: cpuStat().usage_usec };
const sampler = setInterval(() => {
  const now = { t: performance.now(), usage: cpuStat().usage_usec };
  const anon = Number(/^anon (\d+)$/m.exec(read('memory.stat'))?.[1] ?? 0);
  samples.push({ cores: (now.usage - last.usage) / 1000 / (now.t - last.t), memory: Number(read('memory.current')), anon });
  last = now;
}, 500);

const server = await startServer({ port: 0 });
const project = JSON.parse(readFileSync(`${ROOT}fixtures/${projectFile}`, 'utf8'));
const output = `${ROOT}out/s2-${label}.mp4`;
const before = cpuStat();
const started = performance.now();
let run;
try {
  if (pipeline === 'b') {
    run = await renderPage({ server, projectFile, upload: `s2-${label}.mp4`, browserPath: path, size: project.size });
  } else {
    const json = await compileInPage({ server, projectFile, browserPath: path });
    run = await renderServer(json, {
      output,
      browserPath: name === 'chrome' ? undefined : path,
      ffmpeg: pipeline === 'ffmpeg',
      elementCapture: pipeline === 'a-nocapture' ? false : undefined,
    });
  }
} finally {
  clearInterval(sampler);
  await server.close();
}
const wall = (performance.now() - started) / 1000;
const after = cpuStat();
const film = analyseFilm(output);
const limit = read('cpu.max').split(' ');
const cpus = limit[0] === 'max' ? null : Number(limit[0]) / Number(limit[1]);
const busy = samples.filter((s) => cpus !== null && s.cores >= 0.9 * cpus).length;
const record = {
  pipeline,
  browser: name,
  projectFile,
  label,
  cpus,
  memoryMax: read('memory.max'),
  wallSeconds: Number(wall.toFixed(1)),
  renderSeconds: Number(run.seconds.toFixed(1)),
  realTimeFactor: Number((film.info.duration / wall).toFixed(3)),
  meanCores: Number(((after.usage_usec - before.usage_usec) / 1e6 / wall).toFixed(2)),
  samplesAt90PctOfLimit: `${busy}/${samples.length}`,
  throttledPeriods: after.nr_throttled - before.nr_throttled,
  throttledSeconds: Number(((after.throttled_usec - before.throttled_usec) / 1e6).toFixed(1)),
  memoryPeakMiB: Number((Number(read('memory.peak')) / 2 ** 20).toFixed(0)),
  memoryCurrentMaxMiB: Number((Math.max(...samples.map((s) => s.memory)) / 2 ** 20).toFixed(0)),
  // memory.peak and memory.current also count the page cache of files read and written; `anon` is
  // the processes' own memory (memory.stat).
  anonMaxMiB: Number((Math.max(...samples.map((s) => s.anon)) / 2 ** 20).toFixed(0)),
  notFound: server.log.filter((e) => e.status === 404).map((e) => e.path),
  ...summary(film),
  logs: run.logs?.filter((l) => /capture|Opus|error|warn/i.test(l)).slice(0, 10),
};
writeJson(`${ROOT}out/s2-${label}.json`, record);
console.log(JSON.stringify(record, null, 2));

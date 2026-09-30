// S1.1b — does a real render of an H.264/AAC 1080p clip come out with pictures and sound, on each
// browser? renderer-server as shipped, pointed at each binary through VIDEOFLOW_CHROME_PATH
// (ServerRenderer.js:94-97); Chrome is its default `channel: 'chrome'`.
import VideoFlow from '@videoflow/core';
import { browserPaths } from './lib/browsers.mjs';
import { compareFrames, frameAt, levelBetween, meanRgb, probe, windowPowers, writeJson } from './lib/measure.mjs';
import { renderServer } from './lib/render.mjs';
import { ROOT, startServer } from './lib/server.mjs';

const SOURCE = `${ROOT}fixtures/clip-1080p-5s.mp4`;
const server = await startServer({ port: 0 });
const flow = new VideoFlow({ name: 'one-clip', width: 1920, height: 1080, fps: 30, backgroundColor: '#000000' });
flow.addVideo({ fit: 'cover' }, { name: 'clip', source: `${server.origin}/media/clip-1080p-5s.mp4`, startTime: 0, sourceDuration: 5 });
flow.wait(5);
const json = await flow.compile();

const sourceFrame = frameAt(SOURCE, 2.5);
const results = {
  source: { ...probe(SOURCE), level: levelBetween(windowPowers(SOURCE), 0.5, 4.5), frameMeanRgb: meanRgb(sourceFrame) },
};
for (const [name, path] of Object.entries(browserPaths())) {
  if (path === undefined) continue;
  const output = `${ROOT}out/s1-render-${name}.mp4`;
  try {
    const run = await renderServer(json, { output, browserPath: name === 'chrome' ? undefined : path });
    const frame = frameAt(output, 2.5, `${ROOT}out/s1-render-${name}-2.5s.png`);
    results[name] = {
      seconds: Number(run.seconds.toFixed(1)),
      ...probe(output),
      level: levelBetween(windowPowers(output), 0.5, 4.5),
      frameMeanRgb: meanRgb(frame),
      frameVsSource: compareFrames(frame, sourceFrame),
      logs: run.logs.filter((line) => /capture|Opus|audio|encoder|decod|error|warn/i.test(line)).slice(0, 30),
    };
  } catch (error) {
    results[name] = { error: String(error?.message ?? error).slice(0, 2000), logs: error?.logs };
  }
}
await server.close();
writeJson(`${ROOT}out/s1-render.json`, results);
console.log(JSON.stringify(results, null, 2));

// S1.1a — what each browser's WebCodecs says it can decode and encode, asked with
// isConfigSupported (https://www.w3.org/TR/webcodecs/#dom-videodecoder-isconfigsupported), plus
// the <video> element's canPlayType (VideoFlow's decode fallback, RuntimeVideoLayer.js:16-24) and
// whether drawElementImage exists (renderer-server's element-capture path).
import { mkdirSync, writeFileSync } from 'node:fs';
import { browserPaths, launch } from './lib/browsers.mjs';
import { ROOT, startServer } from './lib/server.mjs';

function probe() {
  const video = { width: 1920, height: 1080 };
  const audio = { sampleRate: 48000, numberOfChannels: 2 };
  const ask = async (Api, config) => {
    if (typeof Api === 'undefined') return 'no API';
    try {
      return (await Api.isConfigSupported(config)).supported ? 'yes' : 'no';
    } catch (error) {
      return `throws: ${error.name}`;
    }
  };
  const media = document.createElement('video');
  return (async () => ({
    userAgent: navigator.userAgent,
    videoDecoder: {
      'H.264 High avc1.640028': await ask(self.VideoDecoder, { codec: 'avc1.640028', codedWidth: 1920, codedHeight: 1080 }),
      'HEVC hvc1.1.6.L120.90': await ask(self.VideoDecoder, { codec: 'hvc1.1.6.L120.90', codedWidth: 1920, codedHeight: 1080 }),
      'VP9 vp09.00.40.08': await ask(self.VideoDecoder, { codec: 'vp09.00.40.08', codedWidth: 1920, codedHeight: 1080 }),
      'AV1 av01.0.08M.08': await ask(self.VideoDecoder, { codec: 'av01.0.08M.08', codedWidth: 1920, codedHeight: 1080 }),
    },
    audioDecoder: {
      'AAC mp4a.40.2': await ask(self.AudioDecoder, { codec: 'mp4a.40.2', ...audio }),
      Opus: await ask(self.AudioDecoder, { codec: 'opus', ...audio }),
      MP3: await ask(self.AudioDecoder, { codec: 'mp3', ...audio }),
    },
    videoEncoder: {
      'H.264 High avc1.640028 1080p30': await ask(self.VideoEncoder, { codec: 'avc1.640028', ...video, framerate: 30, bitrate: 8e6 }),
      'H.264 Baseline avc1.42002a 1080p30': await ask(self.VideoEncoder, { codec: 'avc1.42002a', ...video, framerate: 30, bitrate: 8e6 }),
      'VP9 1080p30': await ask(self.VideoEncoder, { codec: 'vp09.00.40.08', ...video, framerate: 30, bitrate: 8e6 }),
      'AV1 1080p30': await ask(self.VideoEncoder, { codec: 'av01.0.08M.08', ...video, framerate: 30, bitrate: 8e6 }),
    },
    audioEncoder: {
      'AAC mp4a.40.2 192k': await ask(self.AudioEncoder, { codec: 'mp4a.40.2', ...audio, bitrate: 192000 }),
      'Opus 128k': await ask(self.AudioEncoder, { codec: 'opus', ...audio, bitrate: 128000 }),
    },
    canPlayType: {
      'mp4 avc1.640028+mp4a.40.2': media.canPlayType('video/mp4; codecs="avc1.640028, mp4a.40.2"') || '""',
      'webm vp9+opus': media.canPlayType('video/webm; codecs="vp9, opus"') || '""',
    },
    drawElementImage: typeof CanvasRenderingContext2D.prototype.drawElementImage === 'function',
  }))();
}

const server = await startServer({ port: 0 });
const results = {};
try {
  for (const [name, path] of Object.entries(browserPaths())) {
    if (path === undefined) {
      results[name] = 'not installed';
      continue;
    }
    const browser = await launch(path);
    try {
      const page = await browser.newPage();
      await page.goto(`${server.origin}/dist/blank.html`);
      results[name] = { executable: path, version: browser.version(), ...(await page.evaluate(probe)) };
    } finally {
      await browser.close();
    }
  }
} finally {
  await server.close();
}
mkdirSync(`${ROOT}out`, { recursive: true });
writeFileSync(`${ROOT}out/s1-codecs.json`, JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));

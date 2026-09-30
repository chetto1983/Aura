// What a rendered file IS, read with ffprobe/ffmpeg rather than with the browser that made it.
// The audio windows repeat web/e2e/support/audioMeasure.ts exactly: mean power over every channel
// of each whole 100 ms window, and a level as the dB of the mean power of the windows inside a span.
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';

export const WINDOW = 0.1;
const RATE = 48000;

export function probe(file) {
  const json = JSON.parse(
    execFileSync('ffprobe', ['-v', 'error', '-count_frames', '-show_streams', '-show_format', '-of', 'json', file]).toString(),
  );
  const video = json.streams.find((s) => s.codec_type === 'video');
  const audio = json.streams.find((s) => s.codec_type === 'audio');
  return {
    container: json.format.format_name,
    duration: Number(json.format.duration),
    bytes: Number(json.format.size),
    video: video && {
      codec: video.codec_name,
      profile: video.profile,
      width: video.width,
      height: video.height,
      fps: video.avg_frame_rate,
      frames: Number(video.nb_read_frames),
      duration: Number(video.duration),
      pixFmt: video.pix_fmt,
    },
    audio: audio && {
      codec: audio.codec_name,
      sampleRate: Number(audio.sample_rate),
      channels: audio.channels,
      duration: Number(audio.duration),
    },
  };
}

export function windowPowers(file) {
  const channels = probe(file).audio?.channels ?? 0;
  if (channels === 0) return [];
  const pcm = execFileSync('ffmpeg', ['-v', 'error', '-i', file, '-vn', '-f', 'f32le', '-acodec', 'pcm_f32le', '-ar', String(RATE), '-'], {
    maxBuffer: 1 << 30,
  });
  const samples = new Float32Array(pcm.buffer, pcm.byteOffset, pcm.byteLength / 4);
  const frames = samples.length / channels;
  const size = Math.round(RATE * WINDOW);
  const powers = [];
  for (let start = 0; start + size <= frames; start += size) {
    let sum = 0;
    for (let at = start * channels; at < (start + size) * channels; at += 1) sum += samples[at] ** 2;
    powers.push(sum / (size * channels));
  }
  return powers;
}

export function levelBetween(powers, from, to) {
  const inside = powers.slice(Math.ceil(from / WINDOW - 1e-9), Math.floor(to / WINDOW + 1e-9));
  if (inside.length === 0) return null;
  const mean = inside.reduce((sum, p) => sum + p, 0) / inside.length;
  return Number((10 * Math.log10(Math.max(mean, 1e-20))).toFixed(2));
}

/** The frame shown at `t` as RGB bytes, and as a PNG beside it for a human to look at. */
export function frameAt(file, t, png) {
  const args = ['-v', 'error', '-ss', String(t), '-i', file, '-frames:v', '1'];
  if (png) execFileSync('ffmpeg', [...args, '-y', png]);
  return execFileSync('ffmpeg', [...args, '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'], { maxBuffer: 1 << 28 });
}

/** PSNR over RGB, and the share of pixels where some channel moved more than 16 of 255. */
export function compareFrames(a, b) {
  if (a.length !== b.length) return { psnr: null, changed: null, note: `sizes differ ${a.length} vs ${b.length}` };
  let squared = 0;
  let changed = 0;
  for (let i = 0; i < a.length; i += 3) {
    let moved = false;
    for (let c = 0; c < 3; c += 1) {
      const d = a[i + c] - b[i + c];
      squared += d * d;
      if (Math.abs(d) > 16) moved = true;
    }
    if (moved) changed += 1;
  }
  const mse = squared / a.length;
  return {
    psnr: mse === 0 ? Infinity : Number((10 * Math.log10((255 * 255) / mse)).toFixed(2)),
    changedPct: Number(((changed / (a.length / 3)) * 100).toFixed(3)),
  };
}

/** Mean of each RGB channel: enough to tell a black or frozen frame from a picture. */
export function meanRgb(rgb) {
  const sum = [0, 0, 0];
  for (let i = 0; i < rgb.length; i += 3) for (let c = 0; c < 3; c += 1) sum[c] += rgb[i + c];
  return sum.map((s) => Number((s / (rgb.length / 3)).toFixed(1)));
}

export function writeJson(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

// The music bed: 8 s of an A-minor progression at a CONSTANT level, 16 kHz mono PCM16. Three
// equal sines of different frequency have an RMS of amp·sqrt(3/2): 0.1225, i.e. -18.24 dBFS.
// A constant level is the point: a volume or a fade is then read straight off the RMS.
import { writeFileSync } from 'node:fs';

const RATE = 16000;
const SECONDS = 8;
const AMP = 0.1;
const CHORDS = [
  [220, 261.63, 329.63],
  [174.61, 220, 261.63],
  [196, 246.94, 293.66],
  [164.81, 207.65, 246.94],
];
const n = RATE * SECONDS;
const wav = Buffer.alloc(44 + n * 2);
wav.write('RIFF', 0);
wav.writeUInt32LE(36 + n * 2, 4);
wav.write('WAVEfmt ', 8);
wav.writeUInt32LE(16, 16);
wav.writeUInt16LE(1, 20);
wav.writeUInt16LE(1, 22);
wav.writeUInt32LE(RATE, 24);
wav.writeUInt32LE(RATE * 2, 28);
wav.writeUInt16LE(2, 32);
wav.writeUInt16LE(16, 34);
wav.write('data', 36);
wav.writeUInt32LE(n * 2, 40);
const phase = [0, 0, 0];
for (let i = 0; i < n; i += 1) {
  const chord = CHORDS[Math.floor(i / (2 * RATE)) % CHORDS.length];
  let value = 0;
  for (let k = 0; k < 3; k += 1) {
    phase[k] += (2 * Math.PI * chord[k]) / RATE;
    value += AMP * Math.sin(phase[k]);
  }
  wav.writeInt16LE(Math.round(value * 32767), 44 + i * 2);
}
writeFileSync(new URL('../../web/e2e/fixtures/video-studio/audio/music.wav', import.meta.url), wav);
console.log(`music.wav: ${wav.length} bytes`);

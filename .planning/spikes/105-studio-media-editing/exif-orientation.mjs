// Writes a copy of a JPEG with a minimal EXIF block holding only Orientation=<n>, the tag a
// phone sets instead of rotating pixels. Usage: node exif-orientation.mjs in.jpg out.jpg 6
import { readFileSync, writeFileSync } from 'node:fs';
const [, , src, dst, value] = process.argv;
const jpeg = readFileSync(src);
if (jpeg[0] !== 0xff || jpeg[1] !== 0xd8) throw new Error('not a JPEG');
const tiff = Buffer.from([
  0x4d, 0x4d, 0x00, 0x2a, 0x00, 0x00, 0x00, 0x08, // big-endian TIFF, IFD0 at 8
  0x00, 0x01, // one entry
  0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, Number(value) >> 8, Number(value) & 0xff, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, // no next IFD
]);
const payload = Buffer.concat([Buffer.from('Exif\0\0', 'binary'), tiff]);
const app1 = Buffer.concat([Buffer.from([0xff, 0xe1, (payload.length + 2) >> 8, (payload.length + 2) & 0xff]), payload]);
writeFileSync(dst, Buffer.concat([jpeg.subarray(0, 2), app1, jpeg.subarray(2)]));

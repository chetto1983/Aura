// cropMath.ts — the crop box of the video editor, in the frame the encoder will see. Mediabunny
// applies `rotate` before `crop` (mediabunny.d.ts, ConversionVideoOptions), and a track's
// display size already includes the rotation its file declares, so the frame is the display
// size turned by the operator's own rotation.

export type Rotation = 0 | 90 | 180 | 270;
export type CropPreset = 'original' | '1:1' | '9:16' | '16:9' | '4:3' | '3:4';
export const CROP_PRESETS: readonly CropPreset[] = [
  'original',
  '1:1',
  '9:16',
  '16:9',
  '4:3',
  '3:4',
];

export interface Size {
  readonly width: number;
  readonly height: number;
}

export interface CropRect {
  readonly left: number;
  readonly top: number;
  readonly width: number;
  readonly height: number;
}

const RATIOS: Readonly<Record<Exclude<CropPreset, 'original'>, readonly [number, number]>> = {
  '1:1': [1, 1],
  '9:16': [9, 16],
  '16:9': [16, 9],
  '4:3': [4, 3],
  '3:4': [3, 4],
};

export function rotateSize(size: Size, rotation: Rotation): Size {
  return rotation === 90 || rotation === 270 ? { width: size.height, height: size.width } : size;
}

// H.264 with 4:2:0 chroma needs even sides.
function evenFloor(value: number): number {
  return Math.max(2, Math.floor(value / 2) * 2);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/** The largest rectangle of the preset's ratio inside `frame`, centred, with even sides. */
export function presetRect(frame: Size, preset: CropPreset): CropRect {
  if (preset === 'original') {
    return { left: 0, top: 0, width: evenFloor(frame.width), height: evenFloor(frame.height) };
  }
  const [w, h] = RATIOS[preset];
  const scale = Math.min(frame.width / w, frame.height / h);
  const width = evenFloor(w * scale);
  const height = evenFloor(h * scale);
  return {
    left: Math.floor((frame.width - width) / 2),
    top: Math.floor((frame.height - height) / 2),
    width,
    height,
  };
}

/** `rect` moved by (dx, dy) frame pixels, kept whole and inside `frame`. */
export function moveRect(rect: CropRect, dx: number, dy: number, frame: Size): CropRect {
  return {
    ...rect,
    left: clamp(Math.round(rect.left + dx), 0, frame.width - rect.width),
    top: clamp(Math.round(rect.top + dy), 0, frame.height - rect.height),
  };
}

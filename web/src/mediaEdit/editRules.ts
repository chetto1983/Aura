export type EditKind = 'image' | 'video';
export type ImageExtension = 'png' | 'jpeg' | 'webp';

const EDITABLE: Readonly<Record<string, EditKind>> = {
  'image/png': 'image',
  'image/jpeg': 'image',
  'image/webp': 'image',
  'video/mp4': 'video',
  'video/webm': 'video',
};

const EDITABLE_EXTENSIONS: Readonly<Record<string, EditKind>> = {
  png: 'image',
  jpg: 'image',
  jpeg: 'image',
  webp: 'image',
  mp4: 'video',
  webm: 'video',
};

function essence(mimeType: string): string {
  return (mimeType.split(';')[0] ?? '').trim().toLowerCase();
}

export function editableKind(mimeType: string): EditKind | undefined {
  return EDITABLE[essence(mimeType)];
}

export function editableKindForFileName(fileName: string): EditKind | undefined {
  const dot = fileName.lastIndexOf('.');
  return dot < 0 ? undefined : EDITABLE_EXTENSIONS[fileName.slice(dot + 1).toLowerCase()];
}

export function imageExtension(mimeType: string): ImageExtension {
  const type = essence(mimeType);
  if (type === 'image/jpeg') return 'jpeg';
  if (type === 'image/webp') return 'webp';
  return 'png';
}

export function editedBase(fileName: string, suffix: string): string {
  const dot = fileName.lastIndexOf('.');
  const base = dot > 0 ? fileName.slice(0, dot) : fileName;
  return `${base}-${suffix}`;
}

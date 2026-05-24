export interface AudioPlaybackPayload {
  audioURL: string;
}

function objectValue(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function audioURLFrom(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  const obj = objectValue(value);
  if (!obj) return '';
  const direct = obj.audio_url ?? obj.audioURL ?? obj.url;
  return typeof direct === 'string' ? direct.trim() : '';
}

export function normalizeAudioPlaybackPayload(input: unknown): AudioPlaybackPayload | null {
  const obj = objectValue(input);
  const name = typeof obj?.name === 'string' ? obj.name.replace(/^data-/, '') : '';
  const candidate = audioURLFrom(obj?.data ?? input);
  if (!candidate) return null;
  if (name && name !== 'audio-url' && name !== 'audio_url' && name !== 'audio') return null;
  if (!candidate.startsWith('/api/chat/audio/')) return null;
  return { audioURL: candidate };
}

// voiceApi — the two HTTP calls the whole voice lane makes, written once: POST /api/stt
// (recorded audio → transcript, transcribe-and-discard server-side) and POST /api/tts
// (text → an object URL over the audio/mpeg body). The dictation adapter, the
// per-message speaker and the hands-free voice overlay all go through here, so the
// wire shape — same-origin cookie, the `audio` multipart field name the Go handler
// reads, the truncation header — has one definition instead of three that drift.
//
// Both throw on a non-2xx. An EMPTY transcript is NOT an error: /api/stt answers a
// clean 200 {"text":""} when the clip held no speech, and the caller decides what that
// means (the overlay keeps listening; the composer shows the retry hint).

export const STT_ROUTE = '/api/stt';
export const TTS_ROUTE = '/api/tts';

/** Set by the backend when the input overflowed AURA_TTS_MAX_CHARS and was cut. */
const TRUNCATED_HEADER = 'X-Aura-TTS-Truncated';

/** POST one recorded clip to /api/stt and return its transcript ("" when silent). */
export async function transcribeAudio(blob: Blob, fileName = 'dictation'): Promise<string> {
  const form = new FormData();
  form.append('audio', blob, fileName);
  const res = await fetch(STT_ROUTE, {
    method: 'POST',
    credentials: 'same-origin',
    body: form,
  });
  if (!res.ok) throw new Error(`stt failed: ${String(res.status)}`);
  const data = (await res.json()) as { text?: unknown };
  return typeof data.text === 'string' ? data.text : '';
}

export interface SynthesizedSpeech {
  /** A blob: URL over the mp3 body. The CALLER owns revoking it. */
  readonly url: string;
  /** True when the backend capped the input text (the D-05 "too long" hint). */
  readonly truncated: boolean;
}

/** POST text to /api/tts and wrap the audio/mpeg body in an object URL. */
export async function synthesizeSpeech(text: string): Promise<SynthesizedSpeech> {
  const res = await fetch(TTS_ROUTE, {
    method: 'POST',
    headers: { Accept: 'audio/mpeg', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ text }),
  });
  if (!res.ok) throw new Error(`tts failed: ${String(res.status)}`);
  const truncated = res.headers.get(TRUNCATED_HEADER) === 'true';
  const blob = await res.blob();
  return { url: URL.createObjectURL(blob), truncated };
}

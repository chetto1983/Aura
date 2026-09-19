// voiceCapture — the microphone the hands-free voice overlay holds OPEN for the whole
// session. One getUserMedia, one audio graph, N recorded clips.
//
// Why the mic stays open instead of being reopened per utterance: the level meter is
// what ends an utterance (silence endpointing) and what detects barge-in WHILE the
// assistant is speaking, so the signal has to keep flowing between clips. Reopening
// also re-runs the browser's gain ramp, which swallows the first syllable of every
// turn — measurable as transcripts that start mid-word.
//
// The meter degrades to silence rather than failing: jsdom (and any browser that
// refuses an AudioContext) simply produces no level samples, so the overlay's manual
// stop button remains the way to end an utterance. Recording itself needs only
// MediaRecorder, which is the part that must work.

/** One recorded clip taken from the already-open stream. */
export interface VoiceRecording {
  /** Stop the clip and resolve its bytes. Resolves an empty blob if nothing was captured. */
  readonly stop: () => Promise<Blob>;
  /** Abandon the clip; its bytes are dropped and stop() resolves empty. */
  readonly cancel: () => void;
}

export interface Microphone {
  /** Subscribe to the 0..1 RMS level. Returns the unsubscribe. */
  readonly onLevel: (listener: (level: number) => void) => () => void;
  /** Record one clip from the open stream. */
  readonly record: () => VoiceRecording;
  /** Release the stream, the meter and the audio graph. Idempotent. */
  readonly close: () => void;
}

/** How often the meter samples the analyser. 60ms is ~16Hz — smooth enough for an orb. */
const METER_INTERVAL_MS = 60;

type AudioContextCtor = new () => AudioContext;

function audioContextCtor(): AudioContextCtor | undefined {
  return (globalThis as { AudioContext?: AudioContextCtor }).AudioContext;
}

/** RMS of one time-domain byte frame, normalized to roughly 0..1 for speech. */
export function frameLevel(frame: Uint8Array): number {
  if (frame.length === 0) return 0;
  let sum = 0;
  for (const sample of frame) {
    const centered = (sample - 128) / 128;
    sum += centered * centered;
  }
  return Math.min(1, Math.sqrt(sum / frame.length) * 4);
}

/** Open the microphone. Rejects when permission is denied or MediaRecorder is absent. */
export async function openMicrophone(): Promise<Microphone> {
  const mediaDevices = (navigator as Partial<Pick<Navigator, 'mediaDevices'>>).mediaDevices;
  if (mediaDevices === undefined) throw new Error('no mediaDevices');
  if (typeof MediaRecorder === 'undefined') throw new Error('no MediaRecorder');
  // Echo cancellation is what makes barge-in mean anything: the mic keeps metering
  // while the assistant speaks, and without it the reply's own audio coming back
  // through the speakers reads as the person talking and cuts the answer off at once.
  // Chromium enables all three by default — stated here because the loop depends on
  // them, not because it is a change of behaviour.
  const stream = await mediaDevices.getUserMedia({
    audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
  });

  const listeners = new Set<(level: number) => void>();
  let closed = false;
  const meter = startMeter(stream, (level) => {
    for (const listener of listeners) listener(level);
  });

  return {
    onLevel: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    record: () => recordClip(stream, () => closed),
    close: () => {
      if (closed) return;
      closed = true;
      listeners.clear();
      meter();
      for (const track of stream.getTracks()) track.stop();
    },
  };
}

/** Wire the analyser and start sampling. Returns the teardown (a no-op with no AudioContext). */
function startMeter(stream: MediaStream, emit: (level: number) => void): () => void {
  const Ctor = audioContextCtor();
  if (Ctor === undefined) return () => undefined;
  let context: AudioContext;
  try {
    context = new Ctor();
  } catch {
    return () => undefined;
  }
  const analyser = context.createAnalyser();
  analyser.fftSize = 1024;
  context.createMediaStreamSource(stream).connect(analyser);
  const frame = new Uint8Array(analyser.fftSize);
  const timer = setInterval(() => {
    analyser.getByteTimeDomainData(frame);
    emit(frameLevel(frame));
  }, METER_INTERVAL_MS);
  return () => {
    clearInterval(timer);
    void context.close().catch(() => undefined);
  };
}

/** Record one clip. `isClosed` guards the window between close() and the recorder's stop. */
function recordClip(stream: MediaStream, isClosed: () => boolean): VoiceRecording {
  const chunks: Blob[] = [];
  const recorder = new MediaRecorder(stream);
  let abandoned = false;
  let settle: ((blob: Blob) => void) | undefined;
  const settled = new Promise<Blob>((resolve) => {
    settle = resolve;
  });

  recorder.ondataavailable = (event) => {
    if (event.data.size > 0 && !abandoned) chunks.push(event.data);
  };
  recorder.onstop = () => {
    settle?.(new Blob(abandoned ? [] : chunks, { type: recorder.mimeType || 'audio/webm' }));
  };
  recorder.start();

  return {
    stop: async () => {
      if (recorder.state !== 'inactive' && !isClosed()) recorder.stop();
      else settle?.(new Blob([], { type: recorder.mimeType || 'audio/webm' }));
      return settled;
    },
    cancel: () => {
      abandoned = true;
      if (recorder.state !== 'inactive') recorder.stop();
      else settle?.(new Blob([], { type: recorder.mimeType || 'audio/webm' }));
    },
  };
}

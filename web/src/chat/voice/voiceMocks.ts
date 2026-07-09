import { vi, type Mock } from 'vitest';

// voiceMocks — the shared vitest doubles for the voice lanes (speaker OUTPUT here in
// 37C-04; dictation INPUT reuses `stubMediaRecorder` + `stubGetUserMedia` in 37C-05).
// jsdom ships no HTMLAudioElement playback, no MediaRecorder, and no
// URL.createObjectURL, so every voice test drives these controllable doubles instead.
// Kept dependency-light (only vitest) and fully exercised by voiceMocks.test.ts so the
// helper itself never drags the suite below the 85% coverage floor.

// ── Audio ─────────────────────────────────────────────────────────────────────

export interface FakeAudioInstance {
  readonly src: string;
  readonly play: Mock<() => Promise<void>>;
  readonly pause: Mock<() => void>;
  onended: (() => void) | null;
  onerror: (() => void) | null;
  /** Fire the `ended` handler the adapter attached (the "finished playing" event). */
  fireEnded: () => void;
  /** Fire the `error` handler the adapter attached (decode/playback failure). */
  fireError: () => void;
}

export interface AudioStub {
  readonly instances: readonly FakeAudioInstance[];
  /** The most recently constructed Audio, or undefined if none was created yet. */
  last: () => FakeAudioInstance | undefined;
  restore: () => void;
}

export interface StubAudioOptions {
  /** When set, `play()` rejects with this error (exercises the autoplay-blocked path). */
  readonly playError?: Error;
}

export function stubAudio(options: StubAudioOptions = {}): AudioStub {
  const instances: FakeAudioInstance[] = [];

  class FakeAudio implements FakeAudioInstance {
    readonly src: string;
    readonly play: Mock<() => Promise<void>>;
    readonly pause: Mock<() => void>;
    onended: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor(src?: string) {
      this.src = src ?? '';
      const { playError } = options;
      this.play = vi.fn<() => Promise<void>>(() =>
        playError === undefined ? Promise.resolve() : Promise.reject(playError),
      );
      this.pause = vi.fn<() => void>();
      instances.push(this);
    }

    fireEnded(): void {
      this.onended?.();
    }

    fireError(): void {
      this.onerror?.();
    }
  }

  vi.stubGlobal('Audio', FakeAudio);
  return {
    instances,
    last: () => instances.at(-1),
    restore: () => {
      vi.unstubAllGlobals();
    },
  };
}

// ── object URLs ─────────────────────────────────────────────────────────────────

export interface ObjectUrlStub {
  readonly createObjectURL: Mock<(blob: Blob | MediaSource) => string>;
  readonly revokeObjectURL: Mock<(url: string) => void>;
  restore: () => void;
}

export function stubObjectURL(): ObjectUrlStub {
  let counter = 0;
  const createObjectURL = vi.fn<(blob: Blob | MediaSource) => string>(
    () => `blob:mock/${String(++counter)}`,
  );
  const revokeObjectURL = vi.fn<(url: string) => void>(() => undefined);
  // jsdom ships no URL.createObjectURL, so define (not assign) the doubles and
  // restore the original descriptor — or delete the property when there was none.
  const createDesc = Object.getOwnPropertyDescriptor(URL, 'createObjectURL');
  const revokeDesc = Object.getOwnPropertyDescriptor(URL, 'revokeObjectURL');
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    writable: true,
    value: createObjectURL,
  });
  Object.defineProperty(URL, 'revokeObjectURL', {
    configurable: true,
    writable: true,
    value: revokeObjectURL,
  });
  return {
    createObjectURL,
    revokeObjectURL,
    restore: () => {
      if (createDesc === undefined) Reflect.deleteProperty(URL, 'createObjectURL');
      else Object.defineProperty(URL, 'createObjectURL', createDesc);
      if (revokeDesc === undefined) Reflect.deleteProperty(URL, 'revokeObjectURL');
      else Object.defineProperty(URL, 'revokeObjectURL', revokeDesc);
    },
  };
}

// ── /api/tts fetch ──────────────────────────────────────────────────────────────

export const TTS_TRUNCATED_HEADER = 'X-Aura-TTS-Truncated';

export interface TtsResponseOptions {
  readonly truncated?: boolean;
  readonly status?: number;
  readonly body?: BlobPart;
}

/** Build a single `POST /api/tts` Response (audio/mpeg blob + optional truncation header). */
export function ttsResponse(options: TtsResponseOptions = {}): Response {
  const headers = new Headers({ 'Content-Type': 'audio/mpeg' });
  if (options.truncated === true) headers.set(TTS_TRUNCATED_HEADER, 'true');
  const blob = new Blob([options.body ?? 'mp3-bytes'], { type: 'audio/mpeg' });
  return new Response(blob, { status: options.status ?? 200, headers });
}

/** A `fetch` double that always answers with one tts Response — asserts the no-second-fetch cache. */
export function mockTtsFetch(options: TtsResponseOptions = {}): Mock<typeof fetch> {
  return vi.fn<typeof fetch>(() => Promise.resolve(ttsResponse(options)));
}

// ── MediaRecorder + getUserMedia (dictation — reused by 37C-05) ───────────────────

export class FakeMediaRecorder {
  static instances: FakeMediaRecorder[] = [];
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  state: 'inactive' | 'recording' = 'inactive';
  readonly mimeType: string;

  constructor(_stream: MediaStream, options?: { mimeType?: string }) {
    this.mimeType = options?.mimeType ?? 'audio/webm';
    FakeMediaRecorder.instances.push(this);
  }

  start(): void {
    this.state = 'recording';
  }

  /** Emit one data chunk then the stop event — the shape the dictation adapter drives. */
  stop(): void {
    this.state = 'inactive';
    this.ondataavailable?.({ data: new Blob(['audio'], { type: this.mimeType }) });
    this.onstop?.();
  }
}

export function stubMediaRecorder(mimeType = 'audio/webm'): typeof FakeMediaRecorder {
  FakeMediaRecorder.instances = [];
  class Recorder extends FakeMediaRecorder {
    constructor(stream: MediaStream, options?: { mimeType?: string }) {
      super(stream, { mimeType: options?.mimeType ?? mimeType });
    }
  }
  vi.stubGlobal('MediaRecorder', Recorder);
  return FakeMediaRecorder;
}

export interface GetUserMediaStub {
  readonly getUserMedia: Mock<(constraints?: MediaStreamConstraints) => Promise<MediaStream>>;
  readonly trackStop: Mock<() => void>;
  restore: () => void;
}

export function stubGetUserMedia(): GetUserMediaStub {
  const trackStop = vi.fn<() => void>();
  const stream = { getTracks: () => [{ stop: trackStop }] } as unknown as MediaStream;
  const getUserMedia = vi.fn<(constraints?: MediaStreamConstraints) => Promise<MediaStream>>(() =>
    Promise.resolve(stream),
  );
  const descriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices');
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  });
  return {
    getUserMedia,
    trackStop,
    restore: () => {
      if (descriptor === undefined) {
        Reflect.deleteProperty(navigator, 'mediaDevices');
      } else {
        Object.defineProperty(navigator, 'mediaDevices', descriptor);
      }
    },
  };
}

// silenceGate — the pure endpointing rule behind hands-free voice: given a stream of
// 0..1 microphone levels with their timestamps, decide when the person has finished
// speaking. No DOM, no timers of its own, time injected by the caller, so the whole
// policy is unit- and mutation-testable.
//
// Two rules, in this order:
//  1. NOTHING ends an utterance before speech has actually been heard. Without this a
//     session that opens on a quiet room would endpoint immediately and POST an empty
//     clip to /api/stt, forever.
//  2. Once speech has been heard, a continuous stretch below the speech level longer
//     than silenceMs ends it — and a single loud frame resets that stretch, so a pause
//     for breath does not cut the sentence in half.
//
// maxUtteranceMs is the backstop for a room that never goes quiet (a fan, a TV): the
// clip is closed anyway rather than recording until the 25 MiB /api/stt ceiling.

export interface SilenceGateOptions {
  /** RMS above which a frame counts as speech. */
  readonly speechLevel?: number;
  /** Continuous quiet, in ms, that ends an utterance once speech was heard. */
  readonly silenceMs?: number;
  /** Hard ceiling, in ms, measured from the first speech frame. */
  readonly maxUtteranceMs?: number;
}

export interface SilenceGate {
  /** Feed one level sample. True EXACTLY ONCE, on the sample that ends the utterance. */
  readonly push: (level: number, atMs: number) => boolean;
  /** Whether any speech has been heard since the last reset. */
  readonly heardSpeech: () => boolean;
  /** Arm the gate for a new utterance. */
  readonly reset: () => void;
}

export const DEFAULT_SPEECH_LEVEL = 0.12;
export const DEFAULT_SILENCE_MS = 1100;
export const DEFAULT_MAX_UTTERANCE_MS = 30_000;

export function createSilenceGate(options: SilenceGateOptions = {}): SilenceGate {
  const speechLevel = options.speechLevel ?? DEFAULT_SPEECH_LEVEL;
  const silenceMs = options.silenceMs ?? DEFAULT_SILENCE_MS;
  const maxUtteranceMs = options.maxUtteranceMs ?? DEFAULT_MAX_UTTERANCE_MS;

  let speechStartedAt: number | undefined;
  let quietSince: number | undefined;
  let ended = false;

  return {
    push: (level, atMs) => {
      if (ended) return false;
      // The ceiling is checked BEFORE the level, or a room that never goes quiet (a
      // fan, a TV) would never reach it — the branch would sit under `level < speech`.
      if (speechStartedAt !== undefined && atMs - speechStartedAt >= maxUtteranceMs) {
        ended = true;
        return true;
      }
      if (level >= speechLevel) {
        speechStartedAt ??= atMs;
        quietSince = undefined;
        return false;
      }
      if (speechStartedAt === undefined) return false;
      quietSince ??= atMs;
      if (atMs - quietSince < silenceMs) return false;
      ended = true;
      return true;
    },
    heardSpeech: () => speechStartedAt !== undefined,
    reset: () => {
      speechStartedAt = undefined;
      quietSince = undefined;
      ended = false;
    },
  };
}

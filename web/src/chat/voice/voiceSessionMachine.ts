// voiceSessionMachine — the hands-free voice loop as a pure reducer. Every transition
// the overlay can make lives here, with no mic, no fetch and no React, so the loop is
// readable in one screen and testable without a browser.
//
//   idle ──start──▶ listening ──endpoint──▶ transcribing ──transcript──▶ thinking
//     ▲                  ▲                        │                          │
//     │                  └────── empty clip ──────┘                        reply
//   close                ▲                                                   │
//     │                  └──── speakEnd / bargeIn ──── speaking ◀────────────┘
//
// The loop closing back onto `listening` is the whole point of hands-free: a reply
// finishes speaking and the mic is live again without a click. `bargeIn` is the same
// edge reached early, when the person starts talking over the answer.
//
// An empty transcript returns to `listening` rather than erroring: a clip with no
// speech in it is a normal event in a room with a door, not a failure to report.

export type VoicePhase = 'idle' | 'listening' | 'transcribing' | 'thinking' | 'speaking' | 'error';

/** Which i18n hint the error phase shows. Kept a symbol, not a sentence. */
export type VoiceErrorKey = 'mic' | 'stt' | 'tts';

export interface VoiceSessionState {
  readonly phase: VoicePhase;
  /** The last transcript sent as a turn — echoed in the overlay so the person sees it. */
  readonly transcript: string;
  /** The reply currently being spoken. */
  readonly reply: string;
  readonly errorKey: VoiceErrorKey | undefined;
}

export type VoiceEvent =
  | { readonly type: 'start' }
  | { readonly type: 'endpoint' }
  | { readonly type: 'transcript'; readonly text: string }
  | { readonly type: 'reply'; readonly text: string }
  | { readonly type: 'speakEnd' }
  | { readonly type: 'bargeIn' }
  | { readonly type: 'fail'; readonly key: VoiceErrorKey }
  | { readonly type: 'close' };

export const initialVoiceSessionState: VoiceSessionState = {
  phase: 'idle',
  transcript: '',
  reply: '',
  errorKey: undefined,
};

/** A fresh listening state: the previous error is cleared, the echoed turn is not. */
function listening(state: VoiceSessionState): VoiceSessionState {
  return { ...state, phase: 'listening', errorKey: undefined };
}

export function voiceSessionReducer(
  state: VoiceSessionState,
  event: VoiceEvent,
): VoiceSessionState {
  switch (event.type) {
    case 'start':
      // Also the retry edge: `start` on `error` is how the overlay's retry button works.
      return state.phase === 'listening' ? state : listening(state);
    case 'endpoint':
      return state.phase === 'listening' ? { ...state, phase: 'transcribing' } : state;
    case 'transcript': {
      if (state.phase !== 'transcribing') return state;
      const text = event.text.trim();
      if (text === '') return listening(state);
      return { ...state, phase: 'thinking', transcript: text, reply: '' };
    }
    case 'reply': {
      if (state.phase !== 'thinking') return state;
      // A run that settled with nothing readable (an error turn, a tool-only answer)
      // has no speech to play, so the loop reopens the mic instead of stalling.
      if (event.text.trim() === '') return listening(state);
      return { ...state, phase: 'speaking', reply: event.text };
    }
    case 'speakEnd':
      return state.phase === 'speaking' ? listening(state) : state;
    case 'bargeIn':
      return state.phase === 'speaking' ? listening(state) : state;
    case 'fail':
      return { ...state, phase: 'error', errorKey: event.key };
    case 'close':
      return initialVoiceSessionState;
  }
}

/** Whether the microphone should be recording a clip in this phase. */
export function isCapturing(phase: VoicePhase): boolean {
  return phase === 'listening';
}

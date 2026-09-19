import { useCallback, useEffect, useReducer, useRef, useState } from 'react';
import { useAui, useAuiState } from '@assistant-ui/react';
import { openMicrophone, type Microphone, type VoiceRecording } from './voiceCapture';
import { createSilenceGate } from './silenceGate';
import { synthesizeSpeech, transcribeAudio } from './voiceApi';
import {
  initialVoiceSessionState,
  isCapturing,
  voiceSessionReducer,
  type VoiceSessionState,
} from './voiceSessionMachine';

// useVoiceSession — the hands-free loop the voice overlay renders. It owns the side
// effects the pure reducer deliberately does not: the open microphone, the /api/stt
// call, the turn it sends, the reply it waits for, and the /api/tts playback.
//
// THE POINT OF THE WHOLE LANE: the transcript is sent through the ordinary composer
// (`setText` + `send`), so a spoken turn is a text turn — same onNew, same persistence,
// same thing the model reads. assistant-ui 0.15's own RealtimeVoiceAdapter cannot do
// this on an external store: measured in the installed bundle, its transcripts are
// appended to a private `_voiceMessages` display list and never reach onNew, so a voice
// turn built on it would render beautifully and arrive nowhere.

/** Consecutive above-threshold samples that count as the person talking over the reply. */
const BARGE_IN_SAMPLES = 3;
const BARGE_IN_LEVEL = 0.18;

export interface VoiceSession extends VoiceSessionState {
  /** 0..1 microphone level, for the orb. Always 0 where no AudioContext exists. */
  readonly level: number;
  /** End the current utterance now (the overlay's stop button). */
  readonly endUtterance: () => void;
  /** Re-arm after an error. */
  readonly retry: () => void;
}

/** The text an assistant message reads aloud: its text parts, joined. Reasoning, tool
 *  calls and sources are not speech and are skipped. */
function assistantText(message: { readonly content: readonly unknown[] } | undefined): string {
  if (message === undefined) return '';
  let spoken = '';
  for (const part of message.content) {
    if (typeof part !== 'object' || part === null) continue;
    const { type, text } = part as { type?: unknown; text?: unknown };
    if (type === 'text' && typeof text === 'string') spoken += text;
  }
  return spoken.trim();
}

export function useVoiceSession(active: boolean): VoiceSession {
  const aui = useAui();
  const messages = useAuiState((s) => s.thread.messages);
  const isRunning = useAuiState((s) => s.thread.isRunning);

  const [state, dispatch] = useReducer(voiceSessionReducer, initialVoiceSessionState);
  const [level, setLevel] = useState(0);

  const micRef = useRef<Microphone | undefined>(undefined);
  const recordingRef = useRef<VoiceRecording | undefined>(undefined);
  const audioRef = useRef<HTMLAudioElement | undefined>(undefined);
  const spokenUrlRef = useRef<string | undefined>(undefined);
  // The newest assistant message id at the moment the turn was sent: the reply is
  // whatever assistant message is newest AFTER the run settles and differs from it.
  const awaitedFromRef = useRef<string | undefined>(undefined);
  const awaitingReplyRef = useRef(false);

  const releaseSpokenUrl = useCallback(() => {
    if (spokenUrlRef.current !== undefined) {
      URL.revokeObjectURL(spokenUrlRef.current);
      spokenUrlRef.current = undefined;
    }
  }, []);

  const stopPlayback = useCallback(() => {
    const audio = audioRef.current;
    if (audio !== undefined) {
      audio.onended = null;
      audio.onerror = null;
      audio.pause();
      audioRef.current = undefined;
    }
    releaseSpokenUrl();
  }, [releaseSpokenUrl]);

  // ── the microphone: opened once for the session, closed with it ──────────────
  useEffect(() => {
    if (!active) return;
    // A holder object read through a FUNCTION (not a bare `let`, not a direct property
    // read) so the flow analysis cannot narrow the cross-closure mutation away and call
    // these guards impossible — the same shape dictationAdapter uses for its cancel flag.
    const live = { disposed: false };
    const disposed = (): boolean => live.disposed;
    void (async () => {
      try {
        const mic = await openMicrophone();
        if (disposed()) {
          mic.close();
          return;
        }
        micRef.current = mic;
        dispatch({ type: 'start' });
      } catch {
        dispatch({ type: 'fail', key: 'mic' });
      }
    })();
    return () => {
      live.disposed = true;
      recordingRef.current?.cancel();
      recordingRef.current = undefined;
      awaitingReplyRef.current = false;
      micRef.current?.close();
      micRef.current = undefined;
      stopPlayback();
      dispatch({ type: 'close' });
    };
  }, [active, stopPlayback]);

  // ── the orb's level, whatever the phase ──────────────────────────────────────
  useEffect(() => {
    const mic = micRef.current;
    if (!active || mic === undefined || state.phase === 'idle') return;
    return mic.onLevel(setLevel);
  }, [active, state.phase]);

  // ── listening: record a clip, end it on silence ──────────────────────────────
  useEffect(() => {
    const mic = micRef.current;
    if (!isCapturing(state.phase) || mic === undefined) return;
    awaitingReplyRef.current = false; // a new utterance awaits a new reply
    const recording = mic.record();
    recordingRef.current = recording;
    const gate = createSilenceGate();
    const unsubscribe = mic.onLevel((sample) => {
      if (gate.push(sample, Date.now())) dispatch({ type: 'endpoint' });
    });
    return () => {
      unsubscribe();
    };
  }, [state.phase]);

  // ── transcribing: stop the clip and POST it ──────────────────────────────────
  useEffect(() => {
    if (state.phase !== 'transcribing') return;
    const recording = recordingRef.current;
    recordingRef.current = undefined;
    const live = { disposed: false };
    const disposed = (): boolean => live.disposed;
    void (async () => {
      try {
        const clip = await (recording?.stop() ?? Promise.resolve(new Blob([])));
        if (disposed()) return;
        const text = clip.size === 0 ? '' : await transcribeAudio(clip, 'voice-turn');
        if (!disposed()) dispatch({ type: 'transcript', text });
      } catch {
        if (!disposed()) dispatch({ type: 'fail', key: 'stt' });
      }
    })();
    return () => {
      live.disposed = true;
    };
  }, [state.phase]);

  // ── thinking: the transcript becomes an ordinary composer turn ───────────────
  //
  // awaitingReplyRef is the send GUARD, not bookkeeping: this effect re-runs on any
  // render that changes `aui`'s identity (assistant-ui's hook is free to return a fresh
  // object), and without the guard each of those re-sent the same sentence as a new
  // turn. Measured in VoiceOverlay.test.tsx, where one spoken sentence produced a second
  // send the moment the reply arrived.
  useEffect(() => {
    if (state.phase !== 'thinking' || state.transcript === '' || awaitingReplyRef.current) return;
    awaitedFromRef.current = [...messages].reverse().find((m) => m.role === 'assistant')?.id;
    awaitingReplyRef.current = true;
    aui.composer.setText(state.transcript);
    aui.composer.send();
    // `messages` is deliberately read, not depended on: the awaited baseline is the
    // thread as it stood at send time.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.phase, state.transcript, aui]);

  // ── the reply: whatever settles after the turn we sent ───────────────────────
  useEffect(() => {
    if (state.phase !== 'thinking' || !awaitingReplyRef.current || isRunning) return;
    const newest = [...messages].reverse().find((m) => m.role === 'assistant');
    if (newest === undefined || newest.id === awaitedFromRef.current) return;
    awaitingReplyRef.current = false;
    dispatch({ type: 'reply', text: assistantText(newest) });
  }, [state.phase, messages, isRunning]);

  // ── speaking: synthesize, play, then reopen the mic ──────────────────────────
  useEffect(() => {
    if (state.phase !== 'speaking' || state.reply === '') return;
    const live = { disposed: false };
    const disposed = (): boolean => live.disposed;
    void (async () => {
      try {
        const { url } = await synthesizeSpeech(state.reply);
        if (disposed()) {
          URL.revokeObjectURL(url);
          return;
        }
        spokenUrlRef.current = url;
        const audio = new Audio(url);
        audioRef.current = audio;
        audio.onended = () => {
          dispatch({ type: 'speakEnd' });
        };
        audio.onerror = () => {
          dispatch({ type: 'fail', key: 'tts' });
        };
        await audio.play();
      } catch {
        if (!disposed()) dispatch({ type: 'fail', key: 'tts' });
      }
    })();
    return () => {
      live.disposed = true;
      stopPlayback();
    };
  }, [state.phase, state.reply, stopPlayback]);

  // ── barge-in: talking over the answer reopens the mic ────────────────────────
  useEffect(() => {
    const mic = micRef.current;
    if (state.phase !== 'speaking' || mic === undefined) return;
    let loud = 0;
    return mic.onLevel((sample) => {
      loud = sample >= BARGE_IN_LEVEL ? loud + 1 : 0;
      if (loud >= BARGE_IN_SAMPLES) dispatch({ type: 'bargeIn' });
    });
  }, [state.phase]);

  const endUtterance = useCallback(() => {
    dispatch({ type: 'endpoint' });
  }, []);
  const retry = useCallback(() => {
    dispatch({ type: 'start' });
  }, []);

  return { ...state, level, endUtterance, retry };
}

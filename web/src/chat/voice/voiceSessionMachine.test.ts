import { describe, expect, it } from 'vitest';
import {
  initialVoiceSessionState,
  isCapturing,
  voiceSessionReducer,
  type VoiceEvent,
  type VoiceSessionState,
} from './voiceSessionMachine';

const drive = (events: readonly VoiceEvent[], from = initialVoiceSessionState): VoiceSessionState =>
  events.reduce(voiceSessionReducer, from);

const LISTENING = drive([{ type: 'start' }]);

describe('voiceSessionReducer', () => {
  it('runs a whole hands-free turn and loops back to listening', () => {
    const spoken = drive([
      { type: 'start' },
      { type: 'endpoint' },
      { type: 'transcript', text: '  che coppia eroga il servo?  ' },
      { type: 'reply', text: 'Ottanta newton per metro.' },
    ]);
    expect(spoken.phase).toBe('speaking');
    expect(spoken.transcript).toBe('che coppia eroga il servo?'); // trimmed
    expect(spoken.reply).toBe('Ottanta newton per metro.');

    const looped = voiceSessionReducer(spoken, { type: 'speakEnd' });
    expect(looped.phase).toBe('listening');
    expect(looped.transcript).toBe('che coppia eroga il servo?'); // the echo survives
  });

  it('an empty transcript reopens the mic instead of erroring', () => {
    const state = drive([
      { type: 'start' },
      { type: 'endpoint' },
      { type: 'transcript', text: '  ' },
    ]);
    expect(state.phase).toBe('listening');
    expect(state.transcript).toBe('');
  });

  it('a run that settles with nothing readable reopens the mic', () => {
    const thinking = drive([
      { type: 'start' },
      { type: 'endpoint' },
      { type: 'transcript', text: 'ciao' },
    ]);
    expect(thinking.phase).toBe('thinking');
    expect(voiceSessionReducer(thinking, { type: 'reply', text: '' }).phase).toBe('listening');
  });

  it('barge-in reopens the mic only while speaking', () => {
    const speaking = drive([
      { type: 'start' },
      { type: 'endpoint' },
      { type: 'transcript', text: 'ciao' },
      { type: 'reply', text: 'buongiorno' },
    ]);
    expect(voiceSessionReducer(speaking, { type: 'bargeIn' }).phase).toBe('listening');
    expect(voiceSessionReducer(LISTENING, { type: 'bargeIn' }).phase).toBe('listening');
    expect(voiceSessionReducer(initialVoiceSessionState, { type: 'bargeIn' }).phase).toBe('idle');
  });

  it('ignores events that do not belong to the current phase', () => {
    expect(voiceSessionReducer(initialVoiceSessionState, { type: 'endpoint' }).phase).toBe('idle');
    expect(voiceSessionReducer(LISTENING, { type: 'transcript', text: 'x' }).phase).toBe(
      'listening',
    );
    expect(voiceSessionReducer(LISTENING, { type: 'reply', text: 'x' }).phase).toBe('listening');
    expect(voiceSessionReducer(LISTENING, { type: 'speakEnd' }).phase).toBe('listening');
  });

  it('start is idempotent on listening and is also the retry edge out of error', () => {
    expect(voiceSessionReducer(LISTENING, { type: 'start' })).toBe(LISTENING);
    const failed = voiceSessionReducer(LISTENING, { type: 'fail', key: 'stt' });
    expect(failed.phase).toBe('error');
    expect(failed.errorKey).toBe('stt');
    const retried = voiceSessionReducer(failed, { type: 'start' });
    expect(retried.phase).toBe('listening');
    expect(retried.errorKey).toBeUndefined();
  });

  it('fail is reachable from every phase, and close resets everything', () => {
    for (const key of ['mic', 'stt', 'tts'] as const) {
      expect(voiceSessionReducer(LISTENING, { type: 'fail', key }).errorKey).toBe(key);
    }
    const spoken = drive([
      { type: 'start' },
      { type: 'endpoint' },
      { type: 'transcript', text: 'ciao' },
      { type: 'reply', text: 'buongiorno' },
    ]);
    expect(voiceSessionReducer(spoken, { type: 'close' })).toEqual(initialVoiceSessionState);
  });

  it('only listening captures audio', () => {
    expect(isCapturing('listening')).toBe(true);
    for (const phase of ['idle', 'transcribing', 'thinking', 'speaking', 'error'] as const) {
      expect(isCapturing(phase)).toBe(false);
    }
  });
});

import { describe, expect, it } from 'vitest';
import { normalizeAudioPlaybackPayload } from './AudioPlayback.helpers';

describe('audio playback helpers', () => {
  it('normalizes assistant-ui data audio frames', () => {
    expect(normalizeAudioPlaybackPayload({
      name: 'audio-url',
      data: { audio_url: '/api/chat/audio/aud_123' },
    })).toEqual({ audioURL: '/api/chat/audio/aud_123' });
  });

  it('accepts ChatReply-style audio_url objects', () => {
    expect(normalizeAudioPlaybackPayload({
      audio_url: '/api/chat/audio/aud_reply',
    })).toEqual({ audioURL: '/api/chat/audio/aud_reply' });
  });

  it('rejects unrelated data frames and external URLs', () => {
    expect(normalizeAudioPlaybackPayload({
      name: 'pending-question',
      data: { audio_url: '/api/chat/audio/aud_123' },
    })).toBeNull();
    expect(normalizeAudioPlaybackPayload({
      name: 'audio-url',
      data: { audio_url: 'https://example.com/audio.ogg' },
    })).toBeNull();
  });
});

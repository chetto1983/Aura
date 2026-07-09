import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, renderHook } from '@testing-library/react';
import { AutoSpeak } from './voice/AutoSpeak';
import { useVoiceRuntime } from './voice/useVoiceRuntime';

// The wiring ExternalStoreChat.tsx delegates to (kept ≤600 LOC): useVoiceRuntime gates the
// two custom adapters on caps and disposes the speech adapter on unmount; AutoSpeak mounts
// the auto-speak effect inside the runtime provider. Both are tested here with the adapter
// factories + voiceMode + useAutoSpeak mocked, so the assertions are deterministic and don't
// drag the full chat mount in (the real runtime attach is exercised by ExternalStoreChat.test
// + the 37C-06 Playwright gate).
const h = vi.hoisted(() => {
  const dispose = vi.fn();
  const caps: { tts: boolean; stt: boolean } = { tts: false, stt: false };
  return {
    dispose,
    speechAdapter: { speak: vi.fn(), dispose },
    dictationAdapter: { listen: vi.fn(), disableInputDuringDictation: true },
    autoSpeak: vi.fn(),
    caps,
  };
});

vi.mock('./voice/speechAdapter', () => ({ createSpeechAdapter: () => h.speechAdapter }));
vi.mock('./voice/dictationAdapter', () => ({ createDictationAdapter: () => h.dictationAdapter }));
vi.mock('./voice/useAutoSpeak', () => ({ useAutoSpeak: h.autoSpeak }));
vi.mock('./voice/voiceModeContext', () => ({
  useVoiceMode: () => ({
    caps: h.caps,
    voiceMode: false,
    turnWasDictated: false,
    toggleVoiceMode: vi.fn(),
    markTurnDictated: vi.fn(),
    clearTurnDictated: vi.fn(),
  }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  h.caps = { tts: false, stt: false };
});

describe('voice runtime wiring (ExternalStoreChat)', () => {
  it('caps={false,false} → the runtime receives NO speech/dictation adapter (native degrade)', () => {
    h.caps = { tts: false, stt: false };
    const { result } = renderHook(() => useVoiceRuntime());
    expect(result.current.speech).toBeUndefined();
    expect(result.current.dictation).toBeUndefined();
  });

  it('caps={true,true} → both custom adapters attach', () => {
    h.caps = { tts: true, stt: true };
    const { result } = renderHook(() => useVoiceRuntime());
    expect(result.current.speech).toBe(h.speechAdapter);
    expect(result.current.dictation).toBe(h.dictationAdapter);
  });

  it('gates each adapter independently (tts only → speech present, dictation absent)', () => {
    h.caps = { tts: true, stt: false };
    const { result } = renderHook(() => useVoiceRuntime());
    expect(result.current.speech).toBe(h.speechAdapter);
    expect(result.current.dictation).toBeUndefined();
  });

  it('invokes speechAdapter.dispose() on unmount — revokes cached object URLs (Landmine #5)', () => {
    h.caps = { tts: true, stt: false };
    const { unmount } = renderHook(() => useVoiceRuntime());
    expect(h.dispose).not.toHaveBeenCalled();
    unmount();
    expect(h.dispose).toHaveBeenCalledTimes(1);
  });

  it('AutoSpeak mounts the auto-speak effect and renders nothing', () => {
    const { container } = render(<AutoSpeak />);
    expect(h.autoSpeak).toHaveBeenCalledTimes(1);
    expect(container.innerHTML).toBe('');
  });
});

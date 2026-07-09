import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { VoiceModeProvider } from './VoiceModeProvider';
import { useVoiceMode } from './voiceModeContext';

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function Probe() {
  const { caps, voiceMode, turnWasDictated, toggleVoiceMode, markTurnDictated, clearTurnDictated } =
    useVoiceMode();
  return (
    <div>
      <span data-testid="state">
        {`${String(voiceMode)}|${String(turnWasDictated)}|${String(caps.tts)}|${String(caps.stt)}`}
      </span>
      <button type="button" onClick={toggleVoiceMode}>
        toggle
      </button>
      <button type="button" onClick={markTurnDictated}>
        mark
      </button>
      <button type="button" onClick={clearTurnDictated}>
        clear
      </button>
    </div>
  );
}

function state(): string {
  return screen.getByTestId('state').textContent ?? '';
}

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe('VoiceModeProvider (ephemeral session state, D-06)', () => {
  it('defaults to false/false and feeds caps from useVoiceCapabilities', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>(() => Promise.resolve(jsonResponse({ tts: true, stt: false }))),
    );
    render(
      <VoiceModeProvider>
        <Probe />
      </VoiceModeProvider>,
    );
    expect(state()).toBe('false|false|false|false');
    await waitFor(() => {
      expect(state()).toBe('false|false|true|false');
    });
  });

  it('toggleVoiceMode flips voiceMode; markTurnDictated/clearTurnDictated drive turnWasDictated', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>(() => Promise.reject(new Error('offline'))),
    );
    render(
      <VoiceModeProvider>
        <Probe />
      </VoiceModeProvider>,
    );
    fireEvent.click(screen.getByText('toggle'));
    expect(state()).toBe('true|false|false|false');
    fireEvent.click(screen.getByText('mark'));
    expect(state()).toBe('true|true|false|false');
    fireEvent.click(screen.getByText('clear'));
    expect(state()).toBe('true|false|false|false');
  });

  it('persists nothing — no localStorage writes (ephemeral only)', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>(() => Promise.reject(new Error('offline'))),
    );
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    render(
      <VoiceModeProvider>
        <Probe />
      </VoiceModeProvider>,
    );
    fireEvent.click(screen.getByText('toggle'));
    fireEvent.click(screen.getByText('mark'));
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
  });

  it('resets on remount — no persisted preference survives', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>(() => Promise.reject(new Error('offline'))),
    );
    const first = render(
      <VoiceModeProvider>
        <Probe />
      </VoiceModeProvider>,
    );
    fireEvent.click(screen.getByText('toggle'));
    expect(state()).toBe('true|false|false|false');
    first.unmount();

    render(
      <VoiceModeProvider>
        <Probe />
      </VoiceModeProvider>,
    );
    expect(state()).toBe('false|false|false|false');
  });
});

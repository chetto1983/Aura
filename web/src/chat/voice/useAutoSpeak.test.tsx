import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { VoiceModeProvider } from './VoiceModeProvider';
import { useVoiceMode } from './voiceModeContext';
import { useAutoSpeak } from './useAutoSpeak';

// A fake external-store runtime: `box` is the mutable thread state the mocked
// useAuiState reads, `speakSpy` records aui.thread().message({id}).speak() calls.
const hoisted = vi.hoisted(() => {
  const speakSpy = vi.fn<(id: string) => void>();
  const box: { messages: { id: string; role: string }[]; isRunning: boolean } = {
    messages: [],
    isRunning: false,
  };
  const auiApi = {
    thread: () => ({
      message: (selector: { id: string }) => ({
        speak: () => {
          speakSpy(selector.id);
        },
      }),
    }),
  };
  return { speakSpy, box, auiApi };
});

vi.mock('@assistant-ui/react', () => ({
  useAui: () => hoisted.auiApi,
  useAuiState: <T,>(selector: (s: { thread: typeof hoisted.box }) => T): T =>
    selector({ thread: hoisted.box }),
}));

function Harness() {
  useAutoSpeak();
  const { voiceMode, turnWasDictated, toggleVoiceMode, markTurnDictated } = useVoiceMode();
  return (
    <div>
      <span data-testid="dictated">{String(turnWasDictated)}</span>
      <span data-testid="voice">{String(voiceMode)}</span>
      <button type="button" onClick={toggleVoiceMode}>
        toggle
      </button>
      <button type="button" onClick={markTurnDictated}>
        mark
      </button>
    </div>
  );
}

// A FRESH element each render — an identical element reference makes React bail out
// of reconciling, so the mocked thread state would never be re-read.
const makeTree = (): ReactElement => (
  <VoiceModeProvider>
    <Harness />
  </VoiceModeProvider>
);

function setThread(messages: { id: string; role: string }[], isRunning = false): void {
  hoisted.box.messages = messages;
  hoisted.box.isRunning = isRunning;
}

function renderHarness() {
  // caps are irrelevant to auto-speak; a rejected probe keeps them disabled.
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>(() => Promise.reject(new Error('no caps'))),
  );
  return render(makeTree());
}

afterEach(() => {
  vi.unstubAllGlobals();
  hoisted.speakSpy.mockClear();
  setThread([]);
});

describe('useAutoSpeak (shouldSpeak-gated, D-07)', () => {
  it('does NOT speak a new reply when voice mode is off and the turn was not dictated', () => {
    const { rerender } = renderHarness();
    setThread([{ id: 'a1', role: 'assistant' }]);
    rerender(makeTree());
    expect(hoisted.speakSpy).not.toHaveBeenCalled();
  });

  it('speaks a new assistant reply once when voice mode is on', () => {
    const { rerender } = renderHarness();
    fireEvent.click(screen.getByText('toggle'));
    setThread([{ id: 'a1', role: 'assistant' }]);
    rerender(makeTree());
    expect(hoisted.speakSpy).toHaveBeenCalledTimes(1);
    expect(hoisted.speakSpy).toHaveBeenCalledWith('a1');
  });

  it('a dictated turn auto-speaks with voice mode off, then is consumed so the next typed turn is silent', () => {
    const { rerender } = renderHarness();
    fireEvent.click(screen.getByText('mark'));
    expect(screen.getByTestId('dictated').textContent).toBe('true');

    setThread([{ id: 'a1', role: 'assistant' }]);
    rerender(makeTree());
    expect(hoisted.speakSpy).toHaveBeenCalledWith('a1');
    // turnWasDictated is consumed for the turn.
    expect(screen.getByTestId('dictated').textContent).toBe('false');

    // A subsequent typed turn (voice mode still off) must NOT auto-speak.
    setThread([
      { id: 'a1', role: 'assistant' },
      { id: 'a2', role: 'assistant' },
    ]);
    rerender(makeTree());
    expect(hoisted.speakSpy).toHaveBeenCalledTimes(1);
  });

  it('never speaks a rehydrated reply already present on mount (init adopts, does not speak)', () => {
    setThread([{ id: 'old', role: 'assistant' }]);
    const { rerender } = renderHarness();
    fireEvent.click(screen.getByText('toggle'));
    rerender(makeTree());
    expect(hoisted.speakSpy).not.toHaveBeenCalled();
  });

  it('waits until the turn settles (isRunning) before speaking', () => {
    const { rerender } = renderHarness();
    fireEvent.click(screen.getByText('toggle'));
    setThread([{ id: 'a1', role: 'assistant' }], true);
    rerender(makeTree());
    expect(hoisted.speakSpy).not.toHaveBeenCalled();
    setThread([{ id: 'a1', role: 'assistant' }], false);
    rerender(makeTree());
    expect(hoisted.speakSpy).toHaveBeenCalledWith('a1');
  });
});

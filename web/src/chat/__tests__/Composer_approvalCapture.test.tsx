import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { Composer } from '../Composer';

type MockDictation = { status: { type: string } } | undefined;

const h = vi.hoisted(() => {
  const composer: {
    dictation: MockDictation;
    isEditing: boolean;
    text: string;
    attachments: never[];
  } = {
    dictation: undefined,
    isEditing: true,
    text: '',
    attachments: [],
  };
  const caps: { tts: boolean; stt: boolean } = { tts: false, stt: false };
  return {
    setText: vi.fn(),
    startDictation: vi.fn(),
    stopDictation: vi.fn(),
    markTurnDictated: vi.fn(),
    auiState: { thread: { isRunning: false, capabilities: { dictation: true } }, composer },
    caps,
  };
});

vi.mock('@assistant-ui/react', async () => ({
  ...(await import('./composerPrimitiveMock')).spread(h),
  useAui: () => ({
    // assistant-ui 0.15 exposes the scopes as PROPERTIES (aui.composer), not calls.
    composer: {
      setText: h.setText,
      startDictation: h.startDictation,
      stopDictation: h.stopDictation,
      getState: () => h.auiState.composer,
    },
  }),
  useAuiState: <T,>(selector: (state: typeof h.auiState) => T): T => selector(h.auiState),
}));

vi.mock('../voice/voiceModeContext', () => ({
  useVoiceMode: () => ({
    caps: h.caps,
    voiceMode: false,
    turnWasDictated: false,
    toggleVoiceMode: vi.fn(),
    markTurnDictated: h.markTurnDictated,
    clearTurnDictated: vi.fn(),
  }),
}));

beforeEach(() => {
  vi.resetAllMocks();
  h.caps = { tts: false, stt: false };
  h.auiState.thread = { isRunning: false, capabilities: { dictation: true } };
  h.auiState.composer = { dictation: undefined, isEditing: true, text: '', attachments: [] };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('Composer approval capture lock', () => {
  it.each(['listening', 'transcribing'] as const)(
    'stops %s dictation and rejects a late transcript from the locked session',
    (phase) => {
      h.caps = { tts: false, stt: true };
      h.auiState.composer.text = 'draft';
      const { rerender } = render(<Composer />);

      fireEvent.click(screen.getByLabelText('Dictate'));
      h.auiState.composer.dictation = { status: { type: 'running' } };
      rerender(<Composer />);
      if (phase === 'transcribing') fireEvent.click(screen.getByLabelText('Stop dictation'));
      const stopCallsBeforeLock = h.stopDictation.mock.calls.length;

      rerender(<Composer approvalLocked />);

      expect(h.stopDictation).toHaveBeenCalledTimes(stopCallsBeforeLock + 1);
      expect(screen.getByRole('button', { name: 'Dictate' })).toHaveProperty('disabled', true);

      h.auiState.composer.text = 'draft late transcript';
      h.auiState.composer.dictation = undefined;
      rerender(<Composer approvalLocked />);

      expect(h.setText).toHaveBeenCalledWith('draft');
      expect(h.markTurnDictated).not.toHaveBeenCalled();
    },
  );
});

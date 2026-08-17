import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { Composer } from '../Composer';

const h = vi.hoisted(() => ({
  setText: vi.fn(),
  addAttachment: vi.fn(),
  // This test never touches the mic, but the shared ComposerPrimitive double renders it, so
  // the handle carries the two spies the double reads rather than the double guessing.
  startDictation: vi.fn(),
  stopDictation: vi.fn(),
  auiState: {
    thread: { isRunning: false },
    composer: { text: '', dictation: undefined, attachments: [] },
  },
}));

vi.mock('@assistant-ui/core/react', async () =>
  (await import('./composerPrimitiveMock')).coreReactMock(h),
);

vi.mock('@assistant-ui/react', async () => ({
  ...(await import('./composerPrimitiveMock')).spread(h),
  useAui: () => ({
    // assistant-ui 0.15 exposes the scopes as PROPERTIES (aui.composer), not calls.
    composer: {
      setText: h.setText,
      startDictation: vi.fn(),
      stopDictation: vi.fn(),
      getState: () => h.auiState.composer,
    },
  }),
  useAuiState: <T,>(selector: (state: typeof h.auiState) => T): T => selector(h.auiState),
}));

vi.mock('../voice/voiceModeContext', () => ({
  useVoiceMode: () => ({
    caps: { tts: false, stt: false },
    voiceMode: false,
    turnWasDictated: false,
    toggleVoiceMode: vi.fn(),
    markTurnDictated: vi.fn(),
    clearTurnDictated: vi.fn(),
  }),
}));

beforeEach(() => {
  vi.resetAllMocks();
  h.auiState.thread = { isRunning: false };
  h.auiState.composer = { text: '', dictation: undefined, attachments: [] };
});

describe('Composer draft prompt ownership', () => {
  it('acknowledges an unlocked draft nonce exactly once after applying it', () => {
    const draftPrompt = { text: 'Answer from release-notes.pdf', nonce: 23 };
    const onDraftPromptConsumed = vi.fn();
    const { rerender } = render(
      <Composer draftPrompt={draftPrompt} onDraftPromptConsumed={onDraftPromptConsumed} />,
    );

    expect(h.setText).toHaveBeenCalledTimes(1);
    expect(h.setText).toHaveBeenCalledWith(draftPrompt.text);
    expect(onDraftPromptConsumed).toHaveBeenCalledTimes(1);
    expect(onDraftPromptConsumed).toHaveBeenCalledWith(draftPrompt.nonce);

    rerender(<Composer draftPrompt={draftPrompt} onDraftPromptConsumed={onDraftPromptConsumed} />);
    expect(h.setText).toHaveBeenCalledTimes(1);
    expect(onDraftPromptConsumed).toHaveBeenCalledTimes(1);
  });

  it('defers a locked draft and applies it exactly once after unlock', () => {
    const draftPrompt = { text: 'Answer from manual.pdf', nonce: 17 };
    const { rerender } = render(<Composer approvalLocked draftPrompt={draftPrompt} />);
    const input = screen.getByLabelText('Ask Aura');
    // The hidden file input belongs to ComposerPrimitive.AddAttachment now; the attach
    // button is the control Aura renders, and it is what must stay untouched here.
    const pickerClicks = vi.fn();
    const sendClicks = vi.fn();
    screen.getByLabelText('Add files').addEventListener('click', pickerClicks);
    screen.getByRole('button', { name: 'Send message' }).addEventListener('click', sendClicks);

    expect(h.setText).not.toHaveBeenCalled();
    expect(input).toHaveProperty('disabled', true);
    expect(document.activeElement).not.toBe(input);

    rerender(<Composer draftPrompt={draftPrompt} />);

    expect(h.setText).toHaveBeenCalledTimes(1);
    expect(h.setText).toHaveBeenCalledWith(draftPrompt.text);
    expect(document.activeElement).toBe(input);
    expect(input).toHaveProperty('disabled', false);
    fireEvent.change(input, { target: { value: 'editable follow-up' } });
    expect(input).toHaveProperty('value', 'editable follow-up');

    rerender(<Composer draftPrompt={draftPrompt} />);
    expect(h.setText).toHaveBeenCalledTimes(1);
    expect(pickerClicks).not.toHaveBeenCalled();
    expect(sendClicks).not.toHaveBeenCalled();
    // Applying a draft must not touch attachments: the adapter is never called.
    expect(h.addAttachment).not.toHaveBeenCalled();
  });
});

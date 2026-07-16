import type { ButtonHTMLAttributes, HTMLAttributes, TextareaHTMLAttributes } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { Composer } from '../Composer';
import type { AttachmentUploads } from '../attachments/useAttachmentUploads';

const h = vi.hoisted(() => ({
  setText: vi.fn(),
  auiState: { thread: { isRunning: false }, composer: { text: '', dictation: undefined } },
}));

vi.mock('@assistant-ui/react', () => ({
  useAui: () => ({
    composer: () => ({
      setText: h.setText,
      startDictation: vi.fn(),
      stopDictation: vi.fn(),
      getState: () => h.auiState.composer,
    }),
  }),
  useAuiState: <T,>(selector: (state: typeof h.auiState) => T): T => selector(h.auiState),
  ComposerPrimitive: {
    Root: (props: HTMLAttributes<HTMLDivElement>) => <div {...props} />,
    Input: (props: TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
    Cancel: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="button" {...props} />,
    Send: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="submit" {...props} />,
  },
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

function uploads(): AttachmentUploads {
  return {
    items: [
      {
        localId: 'manual',
        file: new File(['manual'], 'manual.pdf', { type: 'application/pdf' }),
        progress: 1,
        status: 'ready',
      },
    ],
    readyAssetIds: [],
    hasBlockingUploads: false,
    addFiles: vi.fn(),
    remove: vi.fn(),
    clearReady: vi.fn(),
  };
}

beforeEach(() => {
  vi.resetAllMocks();
  h.auiState.thread = { isRunning: false };
  h.auiState.composer = { text: '', dictation: undefined };
});

describe('Composer draft prompt ownership', () => {
  it('defers a locked draft and applies it exactly once after unlock', () => {
    const draftPrompt = { text: 'Answer from manual.pdf', nonce: 17 };
    const stableUploads = uploads();
    const { container, rerender } = render(
      <Composer approvalLocked draftPrompt={draftPrompt} uploads={stableUploads} />,
    );
    const input = screen.getByLabelText('Ask Aura');
    const fileInput = container.querySelector('input[type="file"]');
    if (!(fileInput instanceof HTMLInputElement)) throw new Error('expected attachment picker');
    const pickerClicks = vi.fn();
    const sendClicks = vi.fn();
    fileInput.addEventListener('click', pickerClicks);
    screen.getByRole('button', { name: 'Send message' }).addEventListener('click', sendClicks);

    expect(h.setText).not.toHaveBeenCalled();
    expect(input).toHaveProperty('disabled', true);
    expect(document.activeElement).not.toBe(input);
    expect(screen.getByText('manual.pdf')).toBeTruthy();

    rerender(<Composer draftPrompt={draftPrompt} uploads={stableUploads} />);

    expect(h.setText).toHaveBeenCalledTimes(1);
    expect(h.setText).toHaveBeenCalledWith(draftPrompt.text);
    expect(document.activeElement).toBe(input);
    expect(input).toHaveProperty('disabled', false);
    expect(screen.getByText('manual.pdf')).toBeTruthy();
    fireEvent.change(input, { target: { value: 'editable follow-up' } });
    expect(input).toHaveProperty('value', 'editable follow-up');

    rerender(<Composer draftPrompt={draftPrompt} uploads={stableUploads} />);
    expect(h.setText).toHaveBeenCalledTimes(1);
    expect(pickerClicks).not.toHaveBeenCalled();
    expect(sendClicks).not.toHaveBeenCalled();
    expect(stableUploads.addFiles).not.toHaveBeenCalled();
    expect(stableUploads.clearReady).not.toHaveBeenCalled();
  });
});

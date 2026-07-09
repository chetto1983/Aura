import type { ButtonHTMLAttributes, HTMLAttributes, TextareaHTMLAttributes } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { Composer } from '../Composer';
import type { AttachmentUploads } from '../attachments/useAttachmentUploads';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from '../voice/voiceMocks';

// Shared mutable doubles for the mocked runtime + voice-mode context. vi.hoisted runs
// before the (hoisted) vi.mock factories, so both can close over `h`; tests mutate
// h.caps / h.auiState to drive the caps.stt branch and the dictation lifecycle.
type MockDictation = { status: { type: string } } | undefined;

const h = vi.hoisted(() => {
  const composer: { dictation: MockDictation; text: string } = { dictation: undefined, text: '' };
  const caps: { tts: boolean; stt: boolean } = { tts: false, stt: false };
  return {
    setText: vi.fn(),
    startDictation: vi.fn(),
    stopDictation: vi.fn(),
    markTurnDictated: vi.fn(),
    auiState: { thread: { isRunning: false }, composer },
    caps,
  };
});

vi.mock('@assistant-ui/react', () => ({
  useAui: () => ({
    composer: () => ({
      setText: h.setText,
      startDictation: h.startDictation,
      stopDictation: h.stopDictation,
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
    caps: h.caps,
    voiceMode: false,
    turnWasDictated: false,
    toggleVoiceMode: vi.fn(),
    markTurnDictated: h.markTurnDictated,
    clearTurnDictated: vi.fn(),
  }),
}));

function uploads(overrides: Partial<AttachmentUploads> = {}): AttachmentUploads {
  return {
    items: [],
    readyAssetIds: [],
    hasBlockingUploads: false,
    addFiles: vi.fn(),
    remove: vi.fn(),
    clearReady: vi.fn(),
    ...overrides,
  };
}

let mediaStub: GetUserMediaStub | undefined;

beforeEach(() => {
  vi.resetAllMocks();
  h.caps = { tts: false, stt: false };
  h.auiState.thread = { isRunning: false };
  h.auiState.composer = { dictation: undefined, text: '' };
});

afterEach(() => {
  mediaStub?.restore();
  mediaStub = undefined;
  vi.unstubAllGlobals();
});

describe('Composer attachments', () => {
  it('prefills a document draft prompt', () => {
    render(<Composer draftPrompt={{ text: 'Answer from Manual.pdf', nonce: 1 }} />);

    expect(h.setText).toHaveBeenCalledWith('Answer from Manual.pdf');
  });

  it('file input passes selected files to addFiles', () => {
    const addFiles = vi.fn();
    const { container } = render(<Composer uploads={uploads({ addFiles })} />);
    const input = container.querySelector('input[type="file"]');
    if (!(input instanceof HTMLInputElement)) throw new Error('expected hidden file input');
    const file = new File(['x'], 'manual.pdf', { type: 'application/pdf' });

    fireEvent.change(input, { target: { files: [file] } });

    expect(Array.from(addFiles.mock.calls[0]?.[0] as FileList | File[])).toEqual([file]);
  });

  it('paste passes clipboard files to addFiles', () => {
    const addFiles = vi.fn();
    const { container } = render(<Composer uploads={uploads({ addFiles })} />);
    const root = container.firstElementChild;
    if (root === null) throw new Error('expected composer root');
    const file = new File(['x'], 'manual.pdf', { type: 'application/pdf' });

    fireEvent.paste(root, { clipboardData: { files: [file] } });

    expect(Array.from(addFiles.mock.calls[0]?.[0] as FileList | File[])).toEqual([file]);
  });

  it('disables send while uploads are blocking', () => {
    render(<Composer uploads={uploads({ hasBlockingUploads: true })} />);

    const send = screen.getByLabelText('Send message');
    expect(send).toHaveProperty('disabled', true);
    expect(send.getAttribute('data-slot')).toBe('button');
  });

  it('keeps the text entry target at the 44px mobile floor', () => {
    render(<Composer />);

    expect(screen.getByLabelText('Ask Aura').className).toContain('min-h-[44px]');
  });
});

describe('Composer dictation', () => {
  it('caps.stt=false → the Mic records an audio attachment (no regression, WEBVOICE-04)', async () => {
    stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    const addFiles = vi.fn();
    render(<Composer uploads={uploads({ addFiles })} />);

    fireEvent.click(screen.getByLabelText('Record audio'));
    await waitFor(() => screen.getByLabelText('Stop recording'));
    fireEvent.click(screen.getByLabelText('Stop recording'));

    expect(mediaStub.getUserMedia).toHaveBeenCalledTimes(1);
    expect(addFiles).toHaveBeenCalledTimes(1);
    expect(h.startDictation).not.toHaveBeenCalled(); // never dictation when STT is off
  });

  it('caps.stt=true → the Mic starts a runtime dictation session (not an attachment)', () => {
    h.caps = { tts: false, stt: true };
    render(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Dictate'));

    expect(h.startDictation).toHaveBeenCalledTimes(1);
  });

  it('marks the turn dictated when a transcript is inserted (auto-speak parity, D-07)', () => {
    h.caps = { tts: false, stt: true };
    h.auiState.composer.text = '';
    const { rerender } = render(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Dictate'));
    // Runtime opens the session (dictation state becomes non-null).
    h.auiState.composer.dictation = { status: { type: 'running' } };
    rerender(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Stop dictation'));
    expect(h.stopDictation).toHaveBeenCalledTimes(1);

    // The adapter's onSpeech inserts the transcript, then the session tears down.
    h.auiState.composer.text = 'hello world';
    h.auiState.composer.dictation = undefined;
    rerender(<Composer uploads={uploads()} />);

    expect(h.markTurnDictated).toHaveBeenCalledTimes(1);
  });

  it('announces listening → transcribing via an aria-live region', () => {
    h.caps = { tts: false, stt: true };
    render(<Composer uploads={uploads()} />);
    const status = screen.getByRole('status');

    fireEvent.click(screen.getByLabelText('Dictate'));
    expect(status.textContent).toContain('Listening');
    expect(status.getAttribute('aria-live')).toBe('polite');

    fireEvent.click(screen.getByLabelText('Stop dictation'));
    expect(status.textContent).toContain('Transcribing');
  });

  it('announces an error when a session ends without inserting a transcript (empty/4xx)', () => {
    h.caps = { tts: false, stt: true };
    h.auiState.composer.text = '';
    const { rerender } = render(<Composer uploads={uploads()} />);
    const status = screen.getByRole('status');

    fireEvent.click(screen.getByLabelText('Dictate'));
    h.auiState.composer.dictation = { status: { type: 'running' } };
    rerender(<Composer uploads={uploads()} />);
    fireEvent.click(screen.getByLabelText('Stop dictation'));

    // Session tears down with the composer text unchanged (nothing was transcribed).
    h.auiState.composer.dictation = undefined;
    rerender(<Composer uploads={uploads()} />);

    expect(h.markTurnDictated).not.toHaveBeenCalled();
    expect(status.textContent).toContain('No transcription');
  });

  it('caps.stt=true but dictation unavailable → degrades to the attachment record path (D-10)', async () => {
    h.caps = { tts: false, stt: true };
    h.startDictation.mockImplementation(() => {
      throw new Error('Dictation adapter not configured');
    });
    stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    render(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Dictate'));

    await waitFor(() => {
      expect(mediaStub?.getUserMedia).toHaveBeenCalledTimes(1);
    });
  });
});

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { Composer } from '../Composer';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from '../voice/voiceMocks';

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
    addAttachment: vi.fn(),
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
      addAttachment: h.addAttachment,
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

function stubPendingGetUserMedia(): GetUserMediaStub & { resolve: () => void } {
  const trackStop = vi.fn<() => void>();
  const stream = { getTracks: () => [{ stop: trackStop }] } as unknown as MediaStream;
  let resolveStream: ((value: MediaStream) => void) | undefined;
  const promise = new Promise<MediaStream>((resolve) => {
    resolveStream = resolve;
  });
  const getUserMedia = vi.fn<(constraints?: MediaStreamConstraints) => Promise<MediaStream>>(
    () => promise,
  );
  const descriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices');
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  });
  return {
    getUserMedia,
    trackStop,
    resolve: () => {
      if (resolveStream === undefined) throw new Error('media stream resolver was not initialized');
      resolveStream(stream);
    },
    restore: () => {
      if (descriptor === undefined) Reflect.deleteProperty(navigator, 'mediaDevices');
      else Object.defineProperty(navigator, 'mediaDevices', descriptor);
    },
  };
}

let mediaStub: GetUserMediaStub | undefined;

beforeEach(() => {
  vi.resetAllMocks();
  h.caps = { tts: false, stt: false };
  h.auiState.thread = { isRunning: false, capabilities: { dictation: true } };
  h.auiState.composer = { dictation: undefined, isEditing: true, text: '', attachments: [] };
});

afterEach(() => {
  mediaStub?.restore();
  mediaStub = undefined;
  vi.unstubAllGlobals();
});

describe('Composer approval capture lock', () => {
  it('aborts an active attachment recorder and suppresses its stop attachment', async () => {
    const Recorder = stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    const { rerender } = render(<Composer />);

    fireEvent.click(screen.getByLabelText('Record audio'));
    await waitFor(() => screen.getByLabelText('Stop recording'));
    const recorder = Recorder.instances[0];
    if (recorder === undefined) throw new Error('expected an active recorder');

    rerender(<Composer approvalLocked />);

    expect(recorder.state).toBe('inactive');
    expect(mediaStub.trackStop).toHaveBeenCalled();
    expect(h.addAttachment).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Record audio' })).toHaveProperty('disabled', true);
  });

  it('stops a stream returned after the composer becomes locked and never creates a recorder', async () => {
    const Recorder = stubMediaRecorder();
    const pendingMedia = stubPendingGetUserMedia();
    mediaStub = pendingMedia;
    const { rerender } = render(<Composer />);

    fireEvent.click(screen.getByLabelText('Record audio'));
    expect(pendingMedia.getUserMedia).toHaveBeenCalledTimes(1);
    rerender(<Composer approvalLocked />);
    await act(async () => {
      pendingMedia.resolve();
      await pendingMedia.getUserMedia.mock.results[0]?.value;
    });

    expect(pendingMedia.trackStop).toHaveBeenCalledTimes(1);
    expect(Recorder.instances).toHaveLength(0);
    expect(h.addAttachment).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('Stop recording')).toBeNull();
  });

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

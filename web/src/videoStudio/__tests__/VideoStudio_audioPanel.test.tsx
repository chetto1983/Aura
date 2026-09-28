import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AudioPanel } from '../VideoStudio_audioPanel';

// The Add audio panel with its four doors. The recorder, the voice and the library are stood in for;
// what is judged is what reaches the workspace's `onSound` or `onLibrary`, and when the panel
// closes.

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
    i18n: { language: 'en' },
  }),
}));

const voice = vi.hoisted(() => ({
  tts: true,
  speak:
    vi.fn<(text: string, signal?: AbortSignal) => Promise<{ blob: Blob; truncated: boolean }>>(),
}));
vi.mock('../../chat/voice/useVoiceCapabilities', () => ({
  useVoiceCapabilities: () => ({ tts: voice.tts, stt: false }),
}));
vi.mock('../../chat/voice/voiceApi', () => ({
  synthesizeSpeechAudio: (text: string, signal?: AbortSignal) => voice.speak(text, signal),
}));
vi.mock('../AudioRecorder', () => ({
  AudioRecorder: ({ onRecorded }: { onRecorded: (file: File) => void }) => (
    <button
      type="button"
      onClick={() => {
        onRecorded(new File(['v'], 'recording.webm', { type: 'audio/webm' }));
      }}
    >
      fake recorder
    </button>
  ),
}));

const BED = { id: 'a1', file_name: 'bed.wav', mime_type: 'audio/wav' };
vi.mock('../VideoStudio_library', () => ({
  LibraryPicker: ({
    modalities,
    onPick,
  }: {
    modalities: readonly string[];
    onPick: (asset: typeof BED) => void;
  }) => (
    <button
      type="button"
      onClick={() => {
        onPick(BED);
      }}
    >
      {`library of ${modalities.join(' and ')}`}
    </button>
  ),
}));

function mount() {
  const onOpenChange = vi.fn();
  const onUpload = vi.fn();
  const onSound = vi.fn((_file: File, _label: string) => Promise.resolve());
  const onLibrary = vi.fn();
  const view = render(
    <AudioPanel
      open
      onOpenChange={onOpenChange}
      onUpload={onUpload}
      onSound={onSound}
      onLibrary={onLibrary}
    />,
  );
  return { ...view, onOpenChange, onUpload, onSound, onLibrary };
}

function speak(text: string) {
  fireEvent.change(screen.getByLabelText('videoStudio.audio.panel.speech'), {
    target: { value: text },
  });
  fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.speak' }));
}

beforeEach(() => {
  voice.tts = true;
  voice.speak.mockReset();
  voice.speak.mockResolvedValue({
    blob: new Blob(['mp3'], { type: 'audio/mpeg' }),
    truncated: false,
  });
});

describe('AudioPanel', () => {
  it('closes and opens the file picker for Upload audio', () => {
    const { onOpenChange, onUpload } = mount();
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.upload' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onUpload).toHaveBeenCalledOnce();
  });

  it('offers the sounds of the library, and closes on the one picked', () => {
    const { onLibrary, onOpenChange, onSound } = mount();
    fireEvent.click(screen.getByRole('button', { name: 'library of audio' }));
    expect(onLibrary).toHaveBeenCalledWith(BED);
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onSound).not.toHaveBeenCalled();
  });

  it('puts a text read aloud on a lane, named after the text, and closes', async () => {
    const { onSound, onOpenChange } = mount();
    speak('  The river runs past the old mill.  ');
    await waitFor(() => {
      expect(onSound).toHaveBeenCalledOnce();
    });
    expect(voice.speak.mock.calls[0]?.[0]).toBe('The river runs past the old mill.');
    const [file, label] = onSound.mock.calls[0] ?? [];
    expect(file?.name).toBe('speech.mp3');
    expect(file?.type).toBe('audio/mpeg');
    expect(label).toBe('The river runs past the old mill.');
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('names a long text by its first forty characters', async () => {
    const { onSound } = mount();
    speak('Nobody expected the concert to start so early, not even the band.');
    await waitFor(() => {
      expect(onSound).toHaveBeenCalledOnce();
    });
    expect(onSound.mock.calls[0]?.[1]).toBe('Nobody expected the concert to start so…');
  });

  it('adds a cut text and stays open to say it was cut', async () => {
    voice.speak.mockResolvedValue({
      blob: new Blob(['mp3'], { type: 'audio/mpeg' }),
      truncated: true,
    });
    const { onSound, onOpenChange } = mount();
    speak('A very long text.');
    expect((await screen.findByRole('status')).textContent).toBe(
      'videoStudio.audio.panel.truncated',
    );
    expect(onSound).toHaveBeenCalledOnce();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it('says why the voice failed and adds nothing', async () => {
    voice.speak.mockRejectedValue(new Error('tts failed: 503'));
    const { onSound } = mount();
    speak('Hello.');
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.panel.speechFailed tts failed: 503',
    );
    expect(onSound).not.toHaveBeenCalled();
  });

  it('offers no text field when this Aura has no voice', () => {
    voice.tts = false;
    mount();
    expect(screen.queryByLabelText('videoStudio.audio.panel.speech')).toBeNull();
    expect(screen.getByRole('button', { name: 'videoStudio.audio.panel.upload' })).toBeTruthy();
  });

  it('does not read an empty text', () => {
    mount();
    expect(
      screen
        .getByRole('button', { name: 'videoStudio.audio.panel.speak' })
        .hasAttribute('disabled'),
    ).toBe(true);
  });

  it('aborts the reading and adds nothing when the panel closes under it', async () => {
    let signal: AbortSignal | undefined;
    voice.speak.mockImplementation((_text, given) => {
      signal = given;
      return new Promise(() => undefined);
    });
    const { onSound, rerender, onOpenChange, onUpload, onLibrary } = mount();
    speak('Hello.');
    rerender(
      <AudioPanel
        open={false}
        onOpenChange={onOpenChange}
        onUpload={onUpload}
        onSound={onSound}
        onLibrary={onLibrary}
      />,
    );
    await waitFor(() => {
      expect(signal?.aborted).toBe(true);
    });
    expect(onSound).not.toHaveBeenCalled();
  });

  it('puts a recording on a lane, named after the hour it was made', () => {
    const { onSound, onOpenChange } = mount();
    fireEvent.click(screen.getByRole('button', { name: 'fake recorder' }));
    const [file, label] = onSound.mock.calls[0] ?? [];
    expect(file?.name).toBe('recording.webm');
    expect(label).toMatch(/^videoStudio\.audio\.panel\.recordingLabel \d{2}:\d{2}/);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});

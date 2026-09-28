import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AudioRecorder } from '../AudioRecorder';

// The recorder with wavesurfer's Record plugin stood in for: the fake answers startRecording as the
// microphone would (or refuses it), and hands its blob to `record-end` as the plugin does — on
// stopRecording, and also on destroy, which is the plugin's own teardown (record.esm.js `destroy`).

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const fake = vi.hoisted(() => ({
  refuse: false,
  blobType: 'audio/webm;codecs=opus',
  plugin: undefined as
    | {
        started: number;
        stopped: number;
        emit: (blob: Blob) => void;
      }
    | undefined,
}));

vi.mock('wavesurfer.js/plugins/record', () => ({
  default: {
    create: () => {
      let listener: ((blob: Blob) => void) | undefined;
      const plugin = {
        started: 0,
        stopped: 0,
        on: (_event: string, callback: (blob: Blob) => void) => {
          listener = callback;
          return () => undefined;
        },
        startRecording: () => {
          plugin.started += 1;
          return fake.refuse
            ? Promise.reject(new Error('Error accessing the microphone: denied'))
            : Promise.resolve();
        },
        stopRecording: () => {
          plugin.stopped += 1;
          plugin.emit(new Blob(['voice'], { type: fake.blobType }));
        },
        emit: (blob: Blob) => listener?.(blob),
      };
      fake.plugin = plugin;
      return plugin;
    },
  },
}));

vi.mock('wavesurfer.js', () => ({
  default: {
    create: () => ({
      // The plugin's teardown hands over what it had, as the real one does.
      destroy: () => fake.plugin?.emit(new Blob(['late'], { type: fake.blobType })),
    }),
  },
}));

beforeEach(() => {
  fake.refuse = false;
  fake.blobType = 'audio/webm;codecs=opus';
  fake.plugin = undefined;
});

function record() {
  fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' }));
}

describe('AudioRecorder', () => {
  it('records, then hands over a file typed by its container alone', async () => {
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    record();
    const stop = await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' });
    expect(fake.plugin?.started).toBe(1);
    expect(screen.getByRole('status').textContent).toBe('videoStudio.audio.panel.recording');
    fireEvent.click(stop);
    expect(fake.plugin?.stopped).toBe(1);
    const file = onRecorded.mock.calls[0]?.[0] as File;
    expect(file.name).toBe('recording.webm');
    expect(file.type).toBe('audio/webm');
    expect(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' })).toBeTruthy();
  });

  it('says a refused microphone in a sentence and records nothing', async () => {
    fake.refuse = true;
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    record();
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.panel.noMicrophone',
    );
    expect(onRecorded).not.toHaveBeenCalled();
  });

  it('drops the recording the plugin hands over when the panel closes mid-way', async () => {
    const onRecorded = vi.fn();
    const view = render(<AudioRecorder onRecorded={onRecorded} />);
    record();
    await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' });
    view.unmount();
    expect(onRecorded).not.toHaveBeenCalled();
  });

  it('types a recording its recorder left untyped as webm, so it is probed as a sound', async () => {
    fake.blobType = '';
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    record();
    fireEvent.click(await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' }));
    const file = onRecorded.mock.calls[0]?.[0] as File | undefined;
    expect(file?.type).toBe('audio/webm');
    expect(file?.name).toBe('recording.webm');
  });

  it('names the file after an ogg container too', async () => {
    fake.blobType = 'audio/ogg';
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    record();
    fireEvent.click(await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' }));
    await waitFor(() => {
      expect((onRecorded.mock.calls[0]?.[0] as File | undefined)?.name).toBe('recording.ogg');
    });
  });
});

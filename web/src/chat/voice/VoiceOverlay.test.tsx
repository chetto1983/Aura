import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { VoiceOverlay } from './VoiceOverlay';
import {
  stubAudio,
  stubGetUserMedia,
  stubMediaRecorder,
  stubObjectURL,
  ttsResponse,
  type AudioStub,
  type GetUserMediaStub,
  type ObjectUrlStub,
} from './voiceMocks';

// The acceptance of the whole lane, in one file: speaking into the overlay must reach
// the model as TEXT through the ordinary composer — `setText` + `send`, the same path a
// typed message takes — and the reply must come back as speech and reopen the mic.
//
// No AudioContext is stubbed on purpose: jsdom has none, so the silence endpointing has
// no levels to read and the utterance is ended by the overlay's own button. That is the
// deterministic half; the level-driven half is covered by silenceGate + voiceCapture.

interface MockMessage {
  id: string;
  role: string;
  content: { type: string; text: string }[];
}

/** The request URL, whatever shape fetch was handed. */
function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  return input instanceof URL ? input.href : input.url;
}

const h = vi.hoisted(() => {
  const box: { messages: MockMessage[]; isRunning: boolean } = { messages: [], isRunning: false };
  return {
    box,
    setText: vi.fn<(text: string) => void>(),
    send: vi.fn<() => void>(),
    toggleVoiceMode: vi.fn<() => void>(),
    caps: { tts: true, stt: true },
    voiceMode: true,
  };
});

vi.mock('@assistant-ui/react', () => ({
  useAui: () => ({ composer: { setText: h.setText, send: h.send } }),
  useAuiState: <T,>(selector: (state: { thread: typeof h.box }) => T): T =>
    selector({ thread: h.box }),
}));

vi.mock('./voiceModeContext', () => ({
  useVoiceMode: () => ({
    caps: h.caps,
    voiceMode: h.voiceMode,
    turnWasDictated: false,
    toggleVoiceMode: h.toggleVoiceMode,
    markTurnDictated: vi.fn(),
    clearTurnDictated: vi.fn(),
  }),
}));

let media: GetUserMediaStub | undefined;
let urls: ObjectUrlStub | undefined;
let audio: AudioStub | undefined;

/** Routes /api/stt and /api/tts; everything else is a hard failure the test will show. */
function stubVoiceFetch(transcript: string, options: { sttStatus?: number } = {}) {
  const fetchMock = vi.fn<typeof fetch>((input) => {
    const url = requestUrl(input);
    if (url.endsWith('/api/stt')) {
      return Promise.resolve(
        options.sttStatus !== undefined && options.sttStatus >= 400
          ? new Response('no', { status: options.sttStatus })
          : Response.json({ text: transcript }),
      );
    }
    if (url.endsWith('/api/tts')) return Promise.resolve(ttsResponse());
    return Promise.reject(new Error(`unexpected fetch: ${url}`));
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

/** An AudioContext whose analyser always reports a loud frame — drives barge-in. */
function stubLoudAudioContext(): void {
  class FakeAudioContext {
    createAnalyser() {
      return {
        fftSize: 0,
        getByteTimeDomainData: (frame: Uint8Array) => frame.fill(220),
      };
    }
    createMediaStreamSource() {
      return { connect: () => undefined };
    }
    close() {
      return Promise.resolve();
    }
  }
  vi.stubGlobal('AudioContext', FakeAudioContext);
}

function settleReply(text: string): void {
  h.box.messages = [{ id: 'a1', role: 'assistant', content: [{ type: 'text', text }] }];
  h.box.isRunning = false;
}

beforeEach(() => {
  h.box.messages = [];
  h.box.isRunning = false;
  h.caps = { tts: true, stt: true };
  h.voiceMode = true;
  h.setText.mockClear();
  h.send.mockClear();
  h.toggleVoiceMode.mockClear();
  stubMediaRecorder();
  media = stubGetUserMedia();
  urls = stubObjectURL();
  audio = stubAudio();
});

afterEach(() => {
  media?.restore();
  urls?.restore();
  audio?.restore();
  media = urls = undefined;
  audio = undefined;
  vi.unstubAllGlobals();
});

describe('VoiceOverlay', () => {
  it('sends the transcript as an ordinary TEXT turn, speaks the reply, then listens again', async () => {
    const fetchMock = stubVoiceFetch('che coppia eroga il servo?');
    const { rerender } = render(<VoiceOverlay />);

    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toBe('Listening…');
    });
    expect(media?.getUserMedia).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole('button', { name: 'Send now' }));

    // THE contract: what crosses to the model is the transcript, not an audio attachment.
    await waitFor(() => {
      expect(h.setText).toHaveBeenCalledWith('che coppia eroga il servo?');
    });
    expect(h.send).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls.map(([url]) => requestUrl(url))).toContain('/api/stt');
    expect(screen.getByTestId('voice-transcript').textContent).toContain(
      'che coppia eroga il servo?',
    );

    settleReply('Ottanta newton per metro.');
    rerender(<VoiceOverlay />);

    await waitFor(() => {
      expect(audio?.last()?.play).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByTestId('voice-reply').textContent).toContain('Ottanta newton per metro.');
    expect(screen.getByTestId('voice-status').textContent).toBe('Answering…');

    audio?.last()?.fireEnded();
    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toBe('Listening…');
    });
  });

  it('sends the turn EXACTLY once, however many times the tree re-renders', async () => {
    stubVoiceFetch('una sola volta');
    const { rerender } = render(<VoiceOverlay />);
    await waitFor(() => screen.getByRole('button', { name: 'Send now' }));

    fireEvent.click(screen.getByRole('button', { name: 'Send now' }));
    await waitFor(() => {
      expect(h.send).toHaveBeenCalledTimes(1);
    });

    // assistant-ui's useAui may hand back a fresh object on every render, so the send
    // effect re-runs; a turn must not be re-sent because the tree redrew.
    for (let i = 0; i < 3; i++) rerender(<VoiceOverlay />);
    h.box.isRunning = true;
    rerender(<VoiceOverlay />);

    expect(h.send).toHaveBeenCalledTimes(1);
    expect(h.setText).toHaveBeenCalledTimes(1);
  });

  it('talking over the answer interrupts it and reopens the mic (barge-in)', async () => {
    stubLoudAudioContext();
    stubVoiceFetch('dimmi qualcosa');
    const { rerender } = render(<VoiceOverlay />);
    await waitFor(() => screen.getByRole('button', { name: 'Send now' }));

    fireEvent.click(screen.getByRole('button', { name: 'Send now' }));
    await waitFor(() => {
      expect(h.send).toHaveBeenCalledTimes(1);
    });

    settleReply('Una risposta molto lunga che nessuno lascia finire.');
    rerender(<VoiceOverlay />);
    await waitFor(() => {
      expect(audio?.last()?.play).toHaveBeenCalledTimes(1);
    });

    // The mic never stopped metering, and it is loud: the reply is cut off.
    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toBe('Listening…');
    });
    expect(audio?.last()?.pause).toHaveBeenCalled();
  });

  it('an empty transcript reopens the mic and sends nothing', async () => {
    stubVoiceFetch('   ');
    render(<VoiceOverlay />);
    await waitFor(() => screen.getByRole('button', { name: 'Send now' }));

    fireEvent.click(screen.getByRole('button', { name: 'Send now' }));

    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toBe('Listening…');
    });
    expect(h.send).not.toHaveBeenCalled();
    expect(screen.queryByTestId('voice-transcript')).toBeNull();
  });

  it('a failing /api/stt stops the loop with a retry, and retry re-arms it', async () => {
    stubVoiceFetch('', { sttStatus: 502 });
    render(<VoiceOverlay />);
    await waitFor(() => screen.getByRole('button', { name: 'Send now' }));

    fireEvent.click(screen.getByRole('button', { name: 'Send now' }));

    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toContain("Couldn't transcribe");
    });
    expect(h.send).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toBe('Listening…');
    });
  });

  it('reports a refused microphone instead of pretending to listen', async () => {
    stubVoiceFetch('x');
    const descriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices');
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: () => Promise.reject(new Error('denied')) },
    });

    render(<VoiceOverlay />);

    await waitFor(() => {
      expect(screen.getByTestId('voice-status').textContent).toContain('No microphone');
    });
    expect(screen.queryByRole('button', { name: 'Send now' })).toBeNull();

    if (descriptor !== undefined) Object.defineProperty(navigator, 'mediaDevices', descriptor);
  });

  it('closes from the button and from Escape, and releases the microphone', async () => {
    stubVoiceFetch('x');
    const { unmount } = render(<VoiceOverlay />);
    await waitFor(() => screen.getByRole('button', { name: 'Send now' }));

    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(h.toggleVoiceMode).toHaveBeenCalledTimes(1);

    // Escape is the dialog's own close, and it must mean the same thing as the button —
    // otherwise the overlay can be dismissed while the microphone stays open.
    fireEvent.keyDown(screen.getByTestId('voice-overlay'), { key: 'Escape', code: 'Escape' });
    await waitFor(() => {
      expect(h.toggleVoiceMode).toHaveBeenCalledTimes(2);
    });

    unmount();
    expect(media?.trackStop).toHaveBeenCalled();
  });

  it('renders nothing at all without both voice legs configured', () => {
    h.caps = { tts: false, stt: true };
    const { container } = render(<VoiceOverlay />);
    expect(container.innerHTML).toBe('');
    expect(screen.queryByTestId('voice-overlay')).toBeNull();
    expect(media?.getUserMedia).not.toHaveBeenCalled();
  });

  it('renders nothing while voice mode is off', () => {
    h.voiceMode = false;
    render(<VoiceOverlay />);
    expect(screen.queryByTestId('voice-overlay')).toBeNull();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  FakeMediaRecorder,
  mockTtsFetch,
  stubAudio,
  stubGetUserMedia,
  stubMediaRecorder,
  stubObjectURL,
  ttsResponse,
  TTS_TRUNCATED_HEADER,
} from './voiceMocks';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('voiceMocks — shared voice test doubles', () => {
  it('stubAudio models play/pause + fires the ended/error handlers', async () => {
    const stub = stubAudio();
    const AudioCtor = globalThis.Audio;
    const audio = new AudioCtor('blob:mock/1');
    expect(stub.last()?.src).toBe('blob:mock/1');
    await audio.play();
    expect(stub.last()?.play).toHaveBeenCalledTimes(1);

    let ended = false;
    let errored = false;
    audio.onended = () => {
      ended = true;
    };
    audio.onerror = () => {
      errored = true;
    };
    stub.last()?.fireEnded();
    stub.last()?.fireError();
    expect(ended).toBe(true);
    expect(errored).toBe(true);

    // No handler attached → fire is a no-op (the ?.() null branch).
    const bare = new AudioCtor();
    expect(bare.src).toBe('');
    stub.instances.at(-1)?.fireEnded();
    stub.instances.at(-1)?.fireError();
    stub.restore();
  });

  it('stubAudio can reject play() (autoplay-blocked path)', async () => {
    stubAudio({ playError: new Error('blocked') });
    const audio = new globalThis.Audio('blob:mock/2');
    await expect(audio.play()).rejects.toThrow('blocked');
  });

  it('stubObjectURL hands out incrementing urls and restores originals', () => {
    const stub = stubObjectURL();
    const a = URL.createObjectURL(new Blob(['x']));
    const b = URL.createObjectURL(new Blob(['y']));
    expect(a).not.toBe(b);
    URL.revokeObjectURL(a);
    expect(stub.createObjectURL).toHaveBeenCalledTimes(2);
    expect(stub.revokeObjectURL).toHaveBeenCalledWith(a);
    stub.restore();
  });

  it('ttsResponse carries audio/mpeg + optional truncation header', async () => {
    const plain = ttsResponse();
    expect(plain.status).toBe(200);
    expect(plain.headers.get('Content-Type')).toBe('audio/mpeg');
    expect(plain.headers.get(TTS_TRUNCATED_HEADER)).toBeNull();

    const capped = ttsResponse({ truncated: true, status: 200, body: 'xx' });
    expect(capped.headers.get(TTS_TRUNCATED_HEADER)).toBe('true');
    expect(await capped.blob()).toBeInstanceOf(Blob);

    const errored = ttsResponse({ status: 400 });
    expect(errored.status).toBe(400);
  });

  it('mockTtsFetch answers with one tts Response', async () => {
    const fetchMock = mockTtsFetch({ truncated: true });
    const res = await fetchMock('/api/tts', { method: 'POST' });
    expect(res.headers.get(TTS_TRUNCATED_HEADER)).toBe('true');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('stubMediaRecorder emits a data chunk then stop, honoring the container mime', () => {
    const Recorder = stubMediaRecorder('audio/mp4');
    expect(Recorder).toBe(FakeMediaRecorder);
    const rec = new globalThis.MediaRecorder({} as MediaStream);
    let chunk: Blob | undefined;
    let stopped = false;
    rec.ondataavailable = (event: BlobEvent) => {
      chunk = event.data;
    };
    rec.onstop = () => {
      stopped = true;
    };
    rec.start();
    expect(FakeMediaRecorder.instances.at(-1)?.state).toBe('recording');
    rec.stop();
    expect(chunk?.type).toBe('audio/mp4');
    expect(stopped).toBe(true);

    // Default container when none is supplied.
    const dflt = new FakeMediaRecorder({} as MediaStream);
    expect(dflt.mimeType).toBe('audio/webm');
  });

  it('stubGetUserMedia resolves a stream with a stoppable track and restores', async () => {
    const stub = stubGetUserMedia();
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    for (const track of stream.getTracks()) track.stop();
    expect(stub.getUserMedia).toHaveBeenCalledWith({ audio: true });
    expect(stub.trackStop).toHaveBeenCalledTimes(1);
    stub.restore();
  });
});

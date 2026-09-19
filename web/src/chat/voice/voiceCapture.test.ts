import { afterEach, describe, expect, it, vi } from 'vitest';
import { frameLevel, openMicrophone } from './voiceCapture';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from './voiceMocks';

let media: GetUserMediaStub | undefined;

afterEach(() => {
  media?.restore();
  media = undefined;
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

/** A minimal AudioContext whose analyser replays a scripted time-domain frame. */
function stubAudioContext(fill: number): { closed: () => boolean } {
  let closed = false;
  class FakeAudioContext {
    createAnalyser() {
      return {
        fftSize: 0,
        getByteTimeDomainData: (frame: Uint8Array) => frame.fill(fill),
      };
    }
    createMediaStreamSource() {
      return { connect: () => undefined };
    }
    close() {
      closed = true;
      return Promise.resolve();
    }
  }
  vi.stubGlobal('AudioContext', FakeAudioContext);
  return { closed: () => closed };
}

describe('frameLevel', () => {
  it('is 0 for a silent frame and 0 for an empty one', () => {
    expect(frameLevel(new Uint8Array(64).fill(128))).toBe(0);
    expect(frameLevel(new Uint8Array(0))).toBe(0);
  });

  it('grows with amplitude and clamps at 1', () => {
    const quiet = frameLevel(new Uint8Array(64).fill(138));
    const loud = frameLevel(new Uint8Array(64).fill(180));
    expect(quiet).toBeGreaterThan(0);
    expect(loud).toBeGreaterThan(quiet);
    expect(frameLevel(new Uint8Array(64).fill(255))).toBe(1);
  });
});

describe('openMicrophone', () => {
  it('refuses when the browser has no mediaDevices', async () => {
    const descriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices');
    Reflect.deleteProperty(navigator, 'mediaDevices');
    stubMediaRecorder();
    await expect(openMicrophone()).rejects.toThrow('mediaDevices');
    if (descriptor !== undefined) Object.defineProperty(navigator, 'mediaDevices', descriptor);
  });

  it('refuses when MediaRecorder is absent', async () => {
    media = stubGetUserMedia();
    vi.stubGlobal('MediaRecorder', undefined);
    await expect(openMicrophone()).rejects.toThrow('MediaRecorder');
  });

  it('records a clip from the open stream and releases the stream only on close', async () => {
    stubMediaRecorder();
    media = stubGetUserMedia();

    const mic = await openMicrophone();
    const clip = await mic.record().stop();

    expect(clip.size).toBeGreaterThan(0);
    // One getUserMedia for the whole session: the mic is not reopened per utterance.
    expect(media.getUserMedia).toHaveBeenCalledTimes(1);
    expect(media.trackStop).not.toHaveBeenCalled();

    mic.close();
    expect(media.trackStop).toHaveBeenCalledTimes(1);
    mic.close(); // idempotent
    expect(media.trackStop).toHaveBeenCalledTimes(1);
  });

  it('a cancelled clip resolves empty — abandoned audio is never transcribed', async () => {
    stubMediaRecorder();
    media = stubGetUserMedia();

    const mic = await openMicrophone();
    const recording = mic.record();
    recording.cancel();

    await expect(recording.stop()).resolves.toMatchObject({ size: 0 });
  });

  it('stopping a clip after close resolves empty instead of hanging', async () => {
    stubMediaRecorder();
    media = stubGetUserMedia();

    const mic = await openMicrophone();
    const recording = mic.record();
    mic.close();

    await expect(recording.stop()).resolves.toBeInstanceOf(Blob);
  });

  it('meters the level to its subscribers and closes the audio graph with the mic', async () => {
    vi.useFakeTimers();
    stubMediaRecorder();
    media = stubGetUserMedia();
    const context = stubAudioContext(200);

    const mic = await openMicrophone();
    const levels: number[] = [];
    const unsubscribe = mic.onLevel((level) => levels.push(level));
    await vi.advanceTimersByTimeAsync(200);

    expect(levels.length).toBeGreaterThan(0);
    expect(levels[0]).toBeGreaterThan(0);

    unsubscribe();
    const seen = levels.length;
    await vi.advanceTimersByTimeAsync(200);
    expect(levels).toHaveLength(seen);

    mic.close();
    expect(context.closed()).toBe(true);
  });

  it('degrades to no meter (never a throw) where an AudioContext cannot be built', async () => {
    vi.useFakeTimers();
    stubMediaRecorder();
    media = stubGetUserMedia();
    function BlockedAudioContext(): never {
      throw new Error('blocked');
    }
    vi.stubGlobal('AudioContext', BlockedAudioContext);

    const mic = await openMicrophone();
    const levels: number[] = [];
    mic.onLevel((level) => levels.push(level));
    await vi.advanceTimersByTimeAsync(500);

    expect(levels).toHaveLength(0);
    mic.close();
  });
});

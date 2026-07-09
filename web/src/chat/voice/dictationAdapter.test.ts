import { afterEach, describe, expect, it, vi, type Mock } from 'vitest';
import type { DictationAdapter } from '@assistant-ui/react';
import { createDictationAdapter } from './dictationAdapter';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from './voiceMocks';

// Drain the adapter's async pipeline (getUserMedia → MediaRecorder, or onstop → POST).
async function tick(): Promise<void> {
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
}

// A single POST /api/stt response — the STT JSON route answers {text}. voiceMocks ships the
// MediaRecorder/getUserMedia doubles (reused here); the STT fetch shape is local to this lane.
function mockSttFetch(options: { text?: string; status?: number } = {}): Mock<typeof fetch> {
  return vi.fn<typeof fetch>(() =>
    Promise.resolve(
      new Response(JSON.stringify({ text: options.text ?? 'hello world' }), {
        status: options.status ?? 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  );
}

describe('createDictationAdapter', () => {
  let media: GetUserMediaStub;

  afterEach(() => {
    media.restore();
    vi.unstubAllGlobals();
  });

  function setup(fetchMock: typeof fetch): DictationAdapter {
    stubMediaRecorder();
    media = stubGetUserMedia();
    vi.stubGlobal('fetch', fetchMock);
    return createDictationAdapter();
  }

  it('disables the input during dictation (brief record window, no interim stream)', () => {
    const adapter = setup(mockSttFetch());
    expect(adapter.disableInputDuringDictation).toBe(true);
  });

  it('records → POSTs the blob to /api/stt → inserts via onSpeech(isFinal:true), THEN onSpeechEnd', async () => {
    const fetchMock = mockSttFetch({ text: 'hello world' });
    const adapter = setup(fetchMock);
    const events: string[] = [];
    const speechResults: DictationAdapter.Result[] = [];

    const session = adapter.listen();
    session.onSpeechStart(() => events.push('start'));
    session.onSpeech((result) => {
      events.push('speech');
      speechResults.push(result);
    });
    session.onSpeechEnd(() => events.push('end'));

    await tick();
    expect(session.status).toEqual({ type: 'running' });
    expect(events).toContain('start');

    await session.stop();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/stt');
    expect(init.method).toBe('POST');
    expect(init.body).toBeInstanceOf(FormData);
    // The transcript is inserted via onSpeech(isFinal:true) — the ONLY path the core writes.
    expect(speechResults).toEqual([{ transcript: 'hello world', isFinal: true }]);
    // onSpeech (insert) MUST fire before onSpeechEnd (cleanup) — RESEARCH Landmine #1.
    expect(events.indexOf('speech')).toBeLessThan(events.indexOf('end'));
    expect(session.status).toEqual({ type: 'ended', reason: 'stopped' });
  });

  it('an empty transcript inserts nothing and still ends clean (reason:stopped)', async () => {
    const adapter = setup(mockSttFetch({ text: '' }));
    const onSpeech = vi.fn();
    const onSpeechEnd = vi.fn();

    const session = adapter.listen();
    session.onSpeech(onSpeech);
    session.onSpeechEnd(onSpeechEnd);

    await tick();
    await session.stop();

    expect(onSpeech).not.toHaveBeenCalled(); // nothing to insert
    expect(onSpeechEnd).toHaveBeenCalledWith({ transcript: '' }); // cleanup still fires
    expect(session.status).toEqual({ type: 'ended', reason: 'stopped' });
  });

  it('a non-ok /api/stt ends the session reason:error with NO onSpeech (Composer degrades)', async () => {
    const adapter = setup(mockSttFetch({ status: 400 }));
    const onSpeech = vi.fn();
    const onSpeechEnd = vi.fn();

    const session = adapter.listen();
    session.onSpeech(onSpeech);
    session.onSpeechEnd(onSpeechEnd);

    await tick();
    await session.stop();

    expect(onSpeech).not.toHaveBeenCalled();
    expect(onSpeechEnd).not.toHaveBeenCalled();
    expect(session.status).toEqual({ type: 'ended', reason: 'error' });
  });

  it('cancel() stops the tracks and ends cancelled without POSTing', async () => {
    const fetchMock = mockSttFetch();
    const adapter = setup(fetchMock);

    const session = adapter.listen();
    await tick();
    session.cancel();

    expect(media.trackStop).toHaveBeenCalled();
    expect(session.status).toEqual({ type: 'ended', reason: 'cancelled' });

    await tick();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

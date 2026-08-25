import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamRun, steerNoticeValue, type AguiFrame } from './sseAdapter';
import { attachRun } from './sseResume';

// The onSteer pump-signal half of the sseAdapter/sseResume suite (Phase 52 plan 07, Task 1).
// Mirrors sseAdapter.onArtifact.test.ts's shape: an `aura.steer` CUSTOM frame drives BOTH the
// driving pump (streamRun, here) AND the reattach pump (sseResume's attachRun) so a reloaded
// tab sees the echo too — asserting the pump fires onSteer with the frame's payload WITHOUT
// emitting from the pure reduceFrame (the steer is already persisted server-side, 52-04).

function sseResponse(frames: readonly AguiFrame[]): Response {
  const enc = new TextEncoder();
  const wire = frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

const RUN_STARTED = { type: 'RUN_STARTED' } as AguiFrame;
const RUN_FINISHED = { type: 'RUN_FINISHED', outcome: { type: 'success' } } as AguiFrame;

function steerFrame(value: unknown): AguiFrame {
  return { type: 'CUSTOM', name: 'aura.steer', value };
}

const VALID_NOTICE = {
  conversation_id: 'conv-1',
  round: 3,
  steers: [
    {
      id: 'steer-1',
      source: 'cockpit',
      text: 'check the invoice first',
      delivery: 'tool_result_append',
    },
  ],
};

describe('steerNoticeValue', () => {
  it('narrows a well-formed aura.steer CUSTOM frame', () => {
    expect(steerNoticeValue(steerFrame(VALID_NOTICE))).toEqual(VALID_NOTICE);
  });

  it('returns null for a CUSTOM frame named aura.artifact', () => {
    expect(
      steerNoticeValue({ type: 'CUSTOM', name: 'aura.artifact', value: VALID_NOTICE }),
    ).toBeNull();
  });

  it('returns null for a malformed aura.steer payload (missing steers array)', () => {
    expect(steerNoticeValue(steerFrame({ conversation_id: 'conv-1', round: 1 }))).toBeNull();
  });

  it('returns null for a malformed aura.steer payload (a steer entry missing delivery)', () => {
    expect(
      steerNoticeValue(
        steerFrame({
          conversation_id: 'conv-1',
          round: 1,
          steers: [{ id: 'x', source: 'cockpit', text: 'hi' }],
        }),
      ),
    ).toBeNull();
  });

  it('returns null for a non-CUSTOM frame', () => {
    expect(steerNoticeValue({ type: 'TEXT_MESSAGE_START', messageId: 'm1' })).toBeNull();
  });
});

describe('sseAdapter — onSteer pump signal on the driving pump (streamRun)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fires onSteer with the notice payload on an aura.steer frame', async () => {
    const onSteer = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(sseResponse([RUN_STARTED, steerFrame(VALID_NOTICE), RUN_FINISHED])),
      ),
    );

    await streamRun({
      threadId: 'conv-1',
      userText: 'redirect this',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onSteer,
    });

    expect(onSteer).toHaveBeenCalledTimes(1);
    expect(onSteer).toHaveBeenCalledWith(VALID_NOTICE);
  });

  it('does not fire for unrelated CUSTOM / text frames', async () => {
    const onSteer = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          sseResponse([
            RUN_STARTED,
            { type: 'CUSTOM', name: 'aura.display', value: { type: 'web_result' } },
            { type: 'TEXT_MESSAGE_START', messageId: 'm1' },
            RUN_FINISHED,
          ]),
        ),
      ),
    );

    await streamRun({
      threadId: 'conv-1',
      userText: 'hi',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onSteer,
    });

    expect(onSteer).not.toHaveBeenCalled();
  });

  it('reduceFrame gains no new message part for an aura.steer frame (byte-equal to today)', async () => {
    const updatesWithout: unknown[] = [];
    const updatesWith: unknown[] = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(sseResponse([RUN_STARTED, RUN_FINISHED]))),
    );
    await streamRun({
      threadId: 'conv-1',
      userText: 'hi',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: (m) => updatesWithout.push(m),
    });

    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(sseResponse([RUN_STARTED, steerFrame(VALID_NOTICE), RUN_FINISHED])),
      ),
    );
    await streamRun({
      threadId: 'conv-1',
      userText: 'hi',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: (m) => updatesWith.push(m),
    });

    const lastWithout = updatesWithout.at(-1);
    const lastWith = updatesWith.at(-1);
    expect(lastWith).toEqual(lastWithout);
  });
});

describe('sseResume — onSteer pump signal on the reattach pump (attachRun)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fires onSteer with the notice payload when a reattached tab observes an aura.steer frame', async () => {
    const onSteer = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(sseResponse([RUN_STARTED, steerFrame(VALID_NOTICE), RUN_FINISHED])),
      ),
    );

    await attachRun({
      threadId: 'conv-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onSteer,
    });

    expect(onSteer).toHaveBeenCalledTimes(1);
    expect(onSteer).toHaveBeenCalledWith(VALID_NOTICE);
  });
});

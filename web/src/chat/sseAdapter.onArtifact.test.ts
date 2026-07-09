import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamRun, type AguiFrame } from './sseAdapter';

// The onArtifact pump-signal half of the sseAdapter suite (37B plan 05). It drives
// an `aura.artifact` CUSTOM frame through streamRun and asserts the pump fires the
// onArtifact callback with the descriptor's asset_id — WITHOUT emitting from the
// pure reduceFrame (T-37B-15). The stream harness mirrors sseAdapter_network.test.ts.

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

function artifactFrame(value: unknown): AguiFrame {
  return { type: 'CUSTOM', name: 'aura.artifact', value };
}

const RUN_STARTED = { type: 'RUN_STARTED' } as AguiFrame;
const RUN_FINISHED = { type: 'RUN_FINISHED', outcome: { type: 'success' } } as AguiFrame;

async function runFrames(
  frames: readonly AguiFrame[],
  onArtifact: (id: string | undefined) => void,
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(sseResponse(frames))),
  );
  await streamRun({
    threadId: 'thread-1',
    userText: 'ship the file',
    signal: new AbortController().signal,
    newId: () => 'fixed-id',
    onUpdate: () => undefined,
    onArtifact,
  });
}

describe('sseAdapter — onArtifact pump signal (37B)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fires onArtifact with the descriptor asset_id on an aura.artifact frame', async () => {
    const onArtifact = vi.fn();
    await runFrames(
      [
        RUN_STARTED,
        artifactFrame({
          tool_call_id: 'call-1',
          filename: 'report.xlsx',
          asset_id: 'asset-9',
          mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
          size_bytes: 4096,
        }),
        RUN_FINISHED,
      ],
      onArtifact,
    );

    expect(onArtifact).toHaveBeenCalledTimes(1);
    expect(onArtifact).toHaveBeenCalledWith('asset-9');
  });

  it('fires onArtifact(undefined) for a valid descriptor missing asset_id (degraded delivery)', async () => {
    const onArtifact = vi.fn();
    await runFrames(
      [RUN_STARTED, artifactFrame({ tool_call_id: 'call-1', filename: 'notes.txt' }), RUN_FINISHED],
      onArtifact,
    );

    expect(onArtifact).toHaveBeenCalledTimes(1);
    expect(onArtifact).toHaveBeenCalledWith(undefined);
  });

  it('does not fire for a malformed aura.artifact descriptor (no tool_call_id/filename)', async () => {
    const onArtifact = vi.fn();
    // The captured golden CUSTOM shape carries path/filename/caption but NO
    // tool_call_id → isArtifactDescriptor rejects it → no card, no signal.
    await runFrames(
      [
        RUN_STARTED,
        artifactFrame({ path: '/abs/results.xlsx', filename: 'results.xlsx', caption: 'results' }),
        RUN_FINISHED,
      ],
      onArtifact,
    );

    expect(onArtifact).not.toHaveBeenCalled();
  });

  it('does not fire for unrelated CUSTOM / text frames', async () => {
    const onArtifact = vi.fn();
    await runFrames(
      [
        RUN_STARTED,
        { type: 'CUSTOM', name: 'aura.display', value: { type: 'web_result' } },
        { type: 'TEXT_MESSAGE_START', messageId: 'm1' },
        { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm1', delta: 'hi' },
        RUN_FINISHED,
      ],
      onArtifact,
    );

    expect(onArtifact).not.toHaveBeenCalled();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamRun, type AguiFrame } from './sseAdapter';

// The onScheduler pump-signal half of the sseAdapter suite: an `aura.scheduler` CUSTOM frame
// means the run CHANGED the schedule, and the cockpit must reread the governance board.
//
// It exists for a measured failure. queryClient.ts sets refetchOnWindowFocus:false for the
// whole SPA and useSchedulerMutations invalidates only on a cockpit approve/run/cancel, so a
// reminder created IN CHAT reached Postgres at 07:31:16, fired, delivered on Telegram, and
// never appeared on the board sitting beside the conversation that created it (2026-09-07).
//
// The frame carries no rows on purpose and the callback takes no argument: the board refetches
// through its own authenticated route, so a chat frame carrying task data would leak one
// surface's authorization into another's.

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

function schedulerFrame(value: unknown): AguiFrame {
  return { type: 'CUSTOM', name: 'aura.scheduler', value };
}

const RUN_STARTED = { type: 'RUN_STARTED' } as AguiFrame;
const RUN_FINISHED = { type: 'RUN_FINISHED', outcome: { type: 'success' } } as AguiFrame;

async function runFrames(frames: readonly AguiFrame[], onScheduler: () => void) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(sseResponse(frames))),
  );
  await streamRun({
    threadId: 'thread-1',
    userText: 'ricordami di telefonare ad Andrea fra 5 minuti',
    signal: new AbortController().signal,
    newId: () => 'fixed-id',
    onUpdate: () => undefined,
    onScheduler,
  });
}

describe('sseAdapter — onScheduler pump signal', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fires onScheduler on an aura.scheduler frame', async () => {
    const onScheduler = vi.fn();
    await runFrames(
      [RUN_STARTED, schedulerFrame({ action: 'schedule' }), RUN_FINISHED],
      onScheduler,
    );

    expect(onScheduler).toHaveBeenCalledTimes(1);
  });

  it('fires once per frame, so two changes in one turn both reach the board', async () => {
    const onScheduler = vi.fn();
    await runFrames(
      [
        RUN_STARTED,
        schedulerFrame({ action: 'schedule' }),
        schedulerFrame({ action: 'cancel' }),
        RUN_FINISHED,
      ],
      onScheduler,
    );

    expect(onScheduler).toHaveBeenCalledTimes(2);
  });

  it('stays silent on a turn that changed no schedule', async () => {
    const onScheduler = vi.fn();
    await runFrames(
      [RUN_STARTED, { type: 'CUSTOM', name: 'aura.artifact', value: {} }, RUN_FINISHED],
      onScheduler,
    );

    expect(onScheduler).not.toHaveBeenCalled();
  });
});

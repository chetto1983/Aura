import { renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import type { SetStateAction } from 'react';
import { useWorkerReportRefresh } from './useWorkerReportRefresh';

// A report refresh replaces the visible thread with the server's. Measured on the lab VM
// 2026-10-27: the card of a photo sent with an earlier turn vanished at that refresh and came
// back only on a page reload, because the snapshot names the turn's files by id alone.

vi.mock('./workerWatchControls', () => ({
  useWatchWorker: () => ({
    statuses: new Map([['w1', { child_id: 'w1', reported: true }]]),
    coordinator: undefined,
  }),
}));

const photo = {
  id: 'asset-photo',
  thread_id: 'conv',
  status: 'complete',
  modality: 'image',
  file_name: 'generated.png',
  mime_type: 'image/png',
  declared_size_bytes: 4,
  size_bytes: 4,
};

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('worker report refresh', () => {
  it('keeps the files a user turn was sent with', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        Promise.resolve(
          url.startsWith('/api/assets?thread_id=conv')
            ? json([photo])
            : json({
                type: 'MESSAGES_SNAPSHOT',
                messages: [
                  {
                    id: 'msg-1',
                    role: 'user',
                    content: 'Modifica la foto allegata',
                    attachmentIds: ['asset-photo'],
                  },
                ],
              }),
        ),
      ),
    );
    const setMessages = vi.fn<(value: SetStateAction<ThreadMessageLike[]>) => void>();
    renderHook(() => {
      useWorkerReportRefresh({
        threadId: 'conv',
        isRunning: false,
        historyRequestRef: { current: 1 },
        setMessages,
      });
    });
    await waitFor(() => {
      expect(setMessages).toHaveBeenCalledTimes(1);
    });
    const update = setMessages.mock.calls[0]?.[0];
    if (typeof update !== 'function') throw new Error('expected a guarded snapshot update');
    const [turn] = update([]);
    expect(turn?.metadata?.custom).toMatchObject({ attachments: [{ id: 'asset-photo' }] });
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { attachRun } from '../sseResume';

// The resume fallback replaces the visible thread with the server's snapshot, which names a
// user turn's files by id only: without the thread's assets the card of a photo sent with
// that turn vanished until a reload (measured on the lab VM 2026-10-27).

const photo = {
  id: 'asset-photo',
  status: 'complete',
  modality: 'image',
  file_name: 'generated.png',
  mime_type: 'image/png',
  declared_size_bytes: 4,
  size_bytes: 4,
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('sseResume — snapshot fallback', () => {
  it('keeps the files a user turn was sent with', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        if (url.startsWith('/agent/runs/')) return Promise.resolve(json({ error: 'gone' }, 404));
        if (url.startsWith('/api/assets?thread_id=thread-1')) return Promise.resolve(json([photo]));
        return Promise.resolve(
          json({
            type: 'MESSAGES_SNAPSHOT',
            messages: [
              { id: 'msg-1', role: 'user', content: 'edit it', attachmentIds: ['asset-photo'] },
            ],
          }),
        );
      }),
    );

    const replaced: ThreadMessageLike[][] = [];
    await attachRun({
      threadId: 'thread-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      maxRetries: 1,
      backoffBaseMs: 0,
      connectionLostNote: 'nota',
      onSnapshotReplace: (m) => replaced.push(m),
      onUpdate: () => undefined,
    });

    expect(replaced[0]?.[0]?.metadata?.custom).toMatchObject({
      attachments: [{ id: 'asset-photo' }],
    });
  });
});

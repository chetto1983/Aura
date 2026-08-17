import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { PendingAttachment } from '@assistant-ui/core';
import { createAuraAttachmentAdapter } from '../auraAttachmentAdapter';
import type { Asset } from '../types';

// The adapter is the whole upload lifecycle now, so the identity it publishes is the thing
// worth pinning: the runtime indexes attachments by id, and this is where a wandering id
// turns one file into two chips.

const { presignAsset, finalizeAsset, deleteAsset, putWithProgress, pollUntilReady } = vi.hoisted(
  () => ({
    presignAsset: vi.fn(),
    finalizeAsset: vi.fn(),
    deleteAsset: vi.fn(),
    putWithProgress: vi.fn(),
    pollUntilReady: vi.fn(),
  }),
);

vi.mock('../api', () => ({ presignAsset, finalizeAsset, deleteAsset, getAsset: vi.fn() }));
vi.mock('../upload', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../upload')>();
  return { ...actual, putWithProgress, pollUntilReady };
});

function asset(overrides: Partial<Asset> = {}): Asset {
  return {
    id: 'asset-1',
    status: 'searchable',
    modality: 'document',
    file_name: 'note.txt',
    mime_type: 'text/plain',
    declared_size_bytes: 4,
    size_bytes: 4,
    ...overrides,
  };
}

async function drain(gen: AsyncGenerator<PendingAttachment, void>): Promise<PendingAttachment[]> {
  const out: PendingAttachment[] = [];
  for await (const value of gen) out.push(value);
  return out;
}

function file(name = 'note.txt', type = 'text/plain'): File {
  return new File(['data'], name, { type });
}

beforeEach(() => {
  vi.clearAllMocks();
  presignAsset.mockResolvedValue({
    asset: asset({ status: 'presigned' }),
    upload: { upload_url: 'https://store.invalid/put', required_headers: {} },
  });
  putWithProgress.mockResolvedValue(undefined);
  finalizeAsset.mockResolvedValue(asset({ status: 'processing' }));
  pollUntilReady.mockResolvedValue(asset());
});

describe('createAuraAttachmentAdapter', () => {
  // Measured live 2026-08-17: the first yield carried the file name as its id and later ones
  // the asset id, so the runtime kept BOTH — one chip frozen at 0%, one at 100%, for a single
  // file. The id must not move once the runtime has seen it.
  it('keeps one identity for the whole upload', async () => {
    const adapter = createAuraAttachmentAdapter({ threadId: 'conv-1' });

    const yields = await drain(
      adapter.add({ file: file() }) as AsyncGenerator<PendingAttachment, void>,
    );

    expect(yields.length).toBeGreaterThan(1);
    const ids = new Set(yields.map((a) => a.id));
    expect(ids.size).toBe(1);
  });

  it('ends ready-for-send and hands the asset back under that same id', async () => {
    const adapter = createAuraAttachmentAdapter({ threadId: 'conv-1' });

    const yields = await drain(
      adapter.add({ file: file() }) as AsyncGenerator<PendingAttachment, void>,
    );
    const last = yields[yields.length - 1];

    expect(last?.status).toEqual({ type: 'requires-action', reason: 'composer-send' });
    // The client id is NOT the asset id; the map is what bridges them for the run envelope.
    expect(last?.id).not.toBe('asset-1');
    expect(adapter.assetFor(last?.id ?? '')?.id).toBe('asset-1');
  });

  it('reports a refused asset as an error the operator can read, not as ready', async () => {
    pollUntilReady.mockResolvedValue(asset({ status: 'refused', error_message: 'troppo grande' }));
    const adapter = createAuraAttachmentAdapter({ threadId: 'conv-1' });

    const yields = await drain(
      adapter.add({ file: file() }) as AsyncGenerator<PendingAttachment, void>,
    );
    const last = yields[yields.length - 1];

    expect(last?.status).toEqual({
      type: 'incomplete',
      reason: 'error',
      message: 'troppo grande',
    });
    expect(adapter.assetFor(last?.id ?? '')).toBeUndefined();
  });

  it('surfaces a presign failure on the same attachment rather than dropping it', async () => {
    presignAsset.mockRejectedValue(new Error('presign refused'));
    const adapter = createAuraAttachmentAdapter({ threadId: 'conv-1' });

    const yields = await drain(
      adapter.add({ file: file() }) as AsyncGenerator<PendingAttachment, void>,
    );

    expect(new Set(yields.map((a) => a.id)).size).toBe(1);
    expect(yields[yields.length - 1]?.status).toEqual({
      type: 'incomplete',
      reason: 'error',
      message: 'presign refused',
    });
  });

  it('deletes the asset on remove and forgets it', async () => {
    deleteAsset.mockResolvedValue(asset({ status: 'deleted' }));
    const adapter = createAuraAttachmentAdapter({ threadId: 'conv-1' });
    const yields = await drain(
      adapter.add({ file: file() }) as AsyncGenerator<PendingAttachment, void>,
    );
    const attachment = yields[yields.length - 1];
    if (attachment === undefined) throw new Error('expected an attachment');

    await adapter.remove(attachment);

    expect(deleteAsset).toHaveBeenCalledWith('asset-1');
    expect(adapter.assetFor(attachment.id)).toBeUndefined();
  });

  it('completes without inlining content — Aura references the asset by id', async () => {
    const adapter = createAuraAttachmentAdapter({ threadId: 'conv-1' });
    const yields = await drain(
      adapter.add({ file: file() }) as AsyncGenerator<PendingAttachment, void>,
    );
    const attachment = yields[yields.length - 1];
    if (attachment === undefined) throw new Error('expected an attachment');

    const complete = await adapter.send(attachment);

    expect(complete.status).toEqual({ type: 'complete' });
    expect(complete.content).toEqual([]);
    expect(complete.id).toBe(attachment.id);
  });
});

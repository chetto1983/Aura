import { describe, expect, it, vi } from 'vitest';
import { createSourceAttachmentAdapter, createSourceAttachmentRunState } from './SourceAttachmentAdapter';

describe('source attachment adapter', () => {
  it('uploads a file before showing the composer chip and sends source metadata', async () => {
    const upload = vi.fn(async (file: File) => ({
      id: 'src_0123456789abcdef',
      status: 'stored' as const,
      duplicate: false,
      filename: file.name,
    }));
    const onSend = vi.fn();
    const adapter = createSourceAttachmentAdapter({ upload, onSend });
    const file = new File(['%PDF-1.4'], 'brief.pdf', { type: 'application/pdf' });

    const pending = await adapter.add({ file });
    expect(upload).toHaveBeenCalledWith(file);
    expect(pending.id).toBe('src_0123456789abcdef');
    expect(pending.name).toBe('brief.pdf');
    expect(pending.status).toEqual({ type: 'requires-action', reason: 'composer-send' });

    const complete = await adapter.send(pending);
    expect(onSend).toHaveBeenCalledWith({
      source_id: 'src_0123456789abcdef',
      filename: 'brief.pdf',
      mime_type: 'application/pdf',
      size: file.size,
    });
    expect(complete.content).toEqual([{
      type: 'file',
      filename: 'brief.pdf',
      data: 'aura-source://src_0123456789abcdef',
      mimeType: 'application/x-aura-source-ref',
    }]);
  });

  it('builds one-shot chat request metadata from sent attachments', () => {
    const state = createSourceAttachmentRunState();
    state.track({ source_id: 'src_0123456789abcdef', filename: 'brief.pdf' });
    state.track({ source_id: 'src_0123456789abcdef', filename: 'brief.pdf' });
    expect(state.extraBody()).toEqual({
      source_ids: ['src_0123456789abcdef'],
      attachments: [{ source_id: 'src_0123456789abcdef', filename: 'brief.pdf' }],
    });
    state.clear();
    expect(state.extraBody()).toBeUndefined();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { exportConversationMarkdown, filenameFromContentDisposition } from '../exportConversation';

// fix-plan 2.10 export leg: the client fetches the server-rendered Markdown and
// downloads it under the Content-Disposition name (RFC-6266: quoted fallback +
// filename*=UTF-8'' extended param — internal/agui/content_disposition.go).

describe('filenameFromContentDisposition', () => {
  it('prefers the RFC-6266 extended UTF-8 param', () => {
    expect(
      filenameFromContentDisposition(
        `attachment; filename="relazione.md"; filename*=UTF-8''relazi%C3%B3ne.md`,
        'fallback.md',
      ),
    ).toBe('relazióne.md');
  });

  it('falls back to the quoted filename when the extended param is absent', () => {
    expect(filenameFromContentDisposition('attachment; filename="plain.md"', 'fallback.md')).toBe(
      'plain.md',
    );
  });

  it('degrades to the fallback on a missing/malformed header', () => {
    expect(filenameFromContentDisposition(null, 'fallback.md')).toBe('fallback.md');
    expect(filenameFromContentDisposition('', 'fallback.md')).toBe('fallback.md');
    expect(filenameFromContentDisposition('attachment', 'fallback.md')).toBe('fallback.md');
    // Bad percent-encoding in the extended param + no quoted form.
    expect(filenameFromContentDisposition(`attachment; filename*=UTF-8''%E0%A4%A`, 'f.md')).toBe(
      'f.md',
    );
  });

  it('skips an empty extended value in favor of the quoted form', () => {
    expect(
      filenameFromContentDisposition(
        `attachment; filename="quoted.md"; filename*=UTF-8''%20`,
        'f.md',
      ),
    ).toBe('quoted.md');
  });
});

describe('exportConversationMarkdown', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('GETs the export endpoint and downloads under the served filename', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response('# Conversation\n\nbody', {
          status: 200,
          headers: {
            'Content-Type': 'application/octet-stream',
            'Content-Disposition': `attachment; filename="my-chat.md"; filename*=UTF-8''my-chat.md`,
          },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    const createUrl = vi.fn(() => 'blob:mock');
    const revokeUrl = vi.fn();
    URL.createObjectURL = createUrl;
    URL.revokeObjectURL = revokeUrl;
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined);

    await exportConversationMarkdown('conv-1');

    expect(fetchMock).toHaveBeenCalledWith('/api/conversations/conv-1/export', {
      method: 'GET',
      credentials: 'same-origin',
    });
    expect(createUrl).toHaveBeenCalledTimes(1);
    expect(click).toHaveBeenCalledTimes(1);
    expect(revokeUrl).toHaveBeenCalledWith('blob:mock');
  });

  it('throws on a non-2xx response (owner-scoped 404) without touching the DOM', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('not found', { status: 404 }))),
    );
    const createUrl = vi.fn(() => 'blob:mock');
    URL.createObjectURL = createUrl;

    await expect(exportConversationMarkdown('foreign')).rejects.toThrow('HTTP 404');
    expect(createUrl).not.toHaveBeenCalled();
  });
});

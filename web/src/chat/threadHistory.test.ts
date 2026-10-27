import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchThreadHistory } from './threadHistory';

const snapshot = {
  type: 'MESSAGES_SNAPSHOT',
  messages: [{ id: 'msg-1', role: 'user', content: 'guarda', attachmentIds: ['asset-photo'] }],
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

describe('fetchThreadHistory', () => {
  it('shows the turns without cards when the asset listing fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        Promise.resolve(
          url.startsWith('/api/assets') ? json({ error: 'down' }, 503) : json(snapshot),
        ),
      ),
    );
    const [turn] = await fetchThreadHistory('conv');
    expect(turn).toMatchObject({ role: 'user', content: [{ type: 'text', text: 'guarda' }] });
    expect(turn?.metadata?.custom).not.toHaveProperty('attachments');
  });

  it('aborts when the asset listing is aborted', async () => {
    const abort = new DOMException('aborted', 'AbortError');
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        url.startsWith('/api/assets') ? Promise.reject(abort) : Promise.resolve(json(snapshot)),
      ),
    );
    await expect(fetchThreadHistory('conv')).rejects.toBe(abort);
  });
});

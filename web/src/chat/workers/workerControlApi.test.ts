import { afterEach, describe, expect, it, vi } from 'vitest';
import { workerControlHistory } from './workerControlApi';

const receipt = {
  id: 'receipt-1',
  run_id: 'run-1',
  text: 'Use JSON',
  status: 'accepted',
  created_at: '2026-09-08T00:00:00Z',
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('worker receipt boundary', () => {
  it('keeps every server-authoritative state and passes the request cancellation signal', async () => {
    const rows = ['accepted', 'applied', 'rejected'].map((status) => ({
      ...receipt,
      id: status,
      status,
    }));
    const fetcher = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ receipts: rows }), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetcher);
    const controller = new AbortController();
    expect(await workerControlHistory('conversation/one', 'child/two', controller.signal)).toEqual(
      rows,
    );
    expect(fetcher).toHaveBeenCalledWith(
      '/api/conversations/conversation%2Fone/swarm/child%2Ftwo/controls',
      { credentials: 'same-origin', signal: controller.signal },
    );
  });

  it.each([
    null,
    [],
    {},
    { receipts: null },
    { receipts: [null] },
    { receipts: [{ ...receipt, status: ['accepted'] }] },
    { receipts: [{ ...receipt, status: 'invented' }] },
    { receipts: [{ ...receipt, run_id: 4 }] },
  ])(
    'refuses a malformed response instead of claiming there are no corrections: %j',
    async (body) => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))),
      );
      await expect(
        workerControlHistory('conversation', 'child', new AbortController().signal),
      ).rejects.toThrow('Invalid worker');
    },
  );

  it.each([401, 404, 503])(
    'does not turn HTTP %d into an empty successful history',
    async (status) => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(new Response('Unavailable', { status }))),
      );
      await expect(
        workerControlHistory('conversation', 'child', new AbortController().signal),
      ).rejects.toThrow();
    },
  );
});

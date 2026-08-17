import { afterEach, describe, expect, it, vi } from 'vitest';
import { CompactionError, NO_COMPACTION, compactConversation, fetchCompaction } from '../api';

// The three failures are distinguishable at the wire and mean different things to the person
// who typed the command: "nothing to condense yet" is not a malfunction, "switched off" is
// not something retrying fixes, everything else is. Collapsing them to one message would
// make the second case look like the third and send the operator looking for a bug.

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('compactConversation', () => {
  it('returns what the compaction did', async () => {
    const fetchMock = vi.fn((url: unknown) => {
      expect(url).toBe('/api/conversations/conv-1/compact');
      return Promise.resolve(
        jsonResponse(200, {
          covers_through_seq: 12,
          source_turns: 9,
          summary: 'the thread so far',
          tokens_before: 41000,
          tokens_after: 2600,
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    await expect(compactConversation('conv-1')).resolves.toMatchObject({
      covers_through_seq: 12,
      source_turns: 9,
    });
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it.each([
    [409, 'nothing'],
    [422, 'notWorthwhile'],
    [503, 'unavailable'],
    [500, 'failed'],
    [404, 'failed'],
  ])('maps HTTP %i to the %s reason', async (status, reason) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(status, {}))),
    );

    await expect(compactConversation('conv-1')).rejects.toMatchObject(
      new CompactionError(reason as 'nothing'),
    );
  });

  it('reports a transport failure as failed rather than throwing something untyped', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('offline'))),
    );

    await expect(compactConversation('conv-1')).rejects.toBeInstanceOf(CompactionError);
  });
});

describe('fetchCompaction', () => {
  it('reads the stored summary', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          jsonResponse(200, { covers_through_seq: 7, source_turns: 5, summary: 'condensed' }),
        ),
      ),
    );

    await expect(fetchCompaction('conv-1')).resolves.toMatchObject({ covers_through_seq: 7 });
  });

  // The marker annotates the transcript. Failing the read must not read as "compacted at
  // seq 0" or take anything else down — it reads as "no marker".
  it('degrades an unreachable read to no compaction', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(500, {}))),
    );

    await expect(fetchCompaction('conv-1')).resolves.toEqual(NO_COMPACTION);
  });
});

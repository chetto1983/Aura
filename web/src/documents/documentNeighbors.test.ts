import { expect, it, vi } from 'vitest';
import { fetchNeighbors } from './documentNeighbors';
import { readDocumentQuery } from './documentRoute';
import { DETAILS } from './documentsTestServer';

it('finds adjacent rows across page boundaries while preserving the whole result query', async () => {
  const calls: URL[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string) => {
      const url = new URL(input, 'http://test');
      calls.push(url);
      const offset = Number(url.searchParams.get('offset'));
      return Response.json({
        rows: Array.from({ length: offset === 25 ? 1 : 25 }, (_, i) => ({
          ...DETAILS.d2,
          id: `d${String(offset + i)}`,
        })),
        total: 26,
        facets: {},
      });
    }),
  );
  try {
    const neighbors = await fetchNeighbors(
      'd24',
      readDocumentQuery('inbox=true&file_type=code&q=Cedar&limit=25&sort=file_name'),
      new AbortController().signal,
    );
    expect(neighbors).toEqual({
      previous: { id: 'd23', offset: 0 },
      next: { id: 'd25', offset: 25 },
    });
    expect(
      calls.every(
        (url) =>
          url.searchParams.get('inbox') === 'true' &&
          url.searchParams.get('file_type') === 'code' &&
          url.searchParams.get('q') === 'Cedar',
      ),
    ).toBe(true);
  } finally {
    vi.unstubAllGlobals();
  }
});

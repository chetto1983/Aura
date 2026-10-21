import { fetchDocuments } from './documentsApi';
import type { DocumentQuery } from './documentRoute';

export interface DocumentNeighbor {
  readonly id: string;
  readonly offset: number;
}
export interface DocumentNeighbors {
  readonly previous?: DocumentNeighbor | undefined;
  readonly next?: DocumentNeighbor | undefined;
}

export async function fetchNeighbors(
  id: string,
  query: DocumentQuery,
  signal: AbortSignal,
): Promise<DocumentNeighbors> {
  const { filter, page } = query;
  let offset = page.offset;
  let result = await fetchDocuments(filter, { ...page, offset }, signal);
  let index = result.rows.findIndex((row) => row.id === id);
  // A copied link can outlive sorting/index changes. Locate its document in the same result.
  if (index < 0) {
    for (let start = 0; start < result.total; start += page.limit) {
      if (start === page.offset) continue;
      result = await fetchDocuments(filter, { ...page, offset: start }, signal);
      index = result.rows.findIndex((row) => row.id === id);
      if (index >= 0) {
        offset = start;
        break;
      }
    }
  }
  if (index < 0) return {};
  const neighbor = (position: number): DocumentNeighbor | undefined => {
    const row = result.rows[position];
    return row ? { id: row.id, offset } : undefined;
  };
  let previous = neighbor(index - 1);
  let next = neighbor(index + 1);
  if (index === 0 && offset > 0) {
    const before = await fetchDocuments(
      filter,
      { ...page, offset: Math.max(0, offset - page.limit) },
      signal,
    );
    const row = before.rows.at(-1);
    if (row) previous = { id: row.id, offset: Math.max(0, offset - page.limit) };
  }
  if (index === result.rows.length - 1 && offset + result.rows.length < result.total) {
    const after = await fetchDocuments(filter, { ...page, offset: offset + page.limit }, signal);
    const row = after.rows[0];
    if (row) next = { id: row.id, offset: offset + page.limit };
  }
  return { previous, next };
}

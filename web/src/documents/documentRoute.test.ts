import { describe, expect, it } from 'vitest';
import { readDocumentQuery, writeDocumentQuery, documentHref, resultHref } from './documentRoute';

describe('document result routes', () => {
  it('round trips filters, paging and display through direct detail links', () => {
    const search =
      '?q=Cedar&search_mode=content&file_type=video&inbox=true&type=t1&type=t2&offset=50&limit=25&sort=-indexed_at&display=cards';
    const state = readDocumentQuery(search);
    const link = documentHref('a/b', '/documents/inbox', state);
    expect(link.startsWith('/documents/document/a%2Fb?')).toBe(true);
    const params = new URLSearchParams(link.split('?')[1]);
    expect(resultHref(params)).toBe(`/documents/inbox?${writeDocumentQuery(state)}`);
    expect(readDocumentQuery(params.toString())).toEqual(state);
  });
  it('defaults to content search and rejects external return destinations', () => {
    expect(readDocumentQuery('').filter.searchMode).toBe('content');
    expect(resultHref(new URLSearchParams('from=https://evil.test'))).toMatch(
      /^\/documents\/library\?/,
    );
    expect(readDocumentQuery('limit=-1&offset=NaN&display=bad').page).toEqual({
      sort: '-indexed_at',
      offset: 0,
      limit: 50,
    });
  });
});

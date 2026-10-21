import { describe, expect, it, vi } from 'vitest';
import {
  bulkApply,
  CountChangedError,
  documentsQuery,
  EMPTY_FILTER,
  sourceOf,
  withField,
  type DocumentsFilter,
} from './documentsApi';

describe('documentsQuery', () => {
  it('explicitly requests Documents and content search for the default library', () => {
    expect(documentsQuery(EMPTY_FILTER, { sort: '', offset: 0, limit: 50 })).toBe(
      'file_type=documents&search_mode=content&offset=0&limit=50',
    );
  });

  it('spells every filter the way the API reads it', () => {
    const filter: DocumentsFilter = {
      ...EMPTY_FILTER,
      include: { type: ['t1', 't2'], counterparty: [], tags: ['g1'] },
      exclude: { type: [], counterparty: ['c9'], tags: [] },
      unassigned: ['tags'],
      tagsMode: 'all',
      dateFrom: '2026-01-01',
      dateTo: '2026-03-31',
      search: ' fattura ',
      needsReview: true,
      includeUnreviewed: false,
    };
    const params = new URLSearchParams(
      documentsQuery(filter, { sort: '-indexed_at', offset: 50, limit: 50 }),
    );
    expect(params.getAll('type')).toEqual(['t1', 't2']);
    expect(params.getAll('tags')).toEqual(['g1']);
    expect(params.getAll('counterparty_not')).toEqual(['c9']);
    expect(params.getAll('unassigned')).toEqual(['tags']);
    expect(params.get('tags_mode')).toBe('all');
    expect(params.get('date_from')).toBe('2026-01-01');
    expect(params.get('date_to')).toBe('2026-03-31');
    expect(params.get('q')).toBe('fattura');
    expect(params.get('needs_review')).toBe('true');
    expect(params.get('include_unreviewed')).toBe('false');
    expect(params.get('sort')).toBe('-indexed_at');
    expect(params.get('offset')).toBe('50');
  });
});

describe('withField', () => {
  it('adds a value to a field, takes it out of the opposite list, and toggles it off', () => {
    const excluded: DocumentsFilter = {
      ...EMPTY_FILTER,
      exclude: { ...EMPTY_FILTER.exclude, type: ['t1'] },
    };
    const included = withField(excluded, 'include', 'type', 't1');
    expect(included.include.type).toEqual(['t1']);
    expect(included.exclude.type).toEqual([]);
    expect(withField(included, 'include', 'type', 't1').include.type).toEqual([]);
  });
});

describe('sourceOf', () => {
  it('names who set a value, and nothing for the operator', () => {
    expect(sourceOf('operator')).toBeNull();
    expect(sourceOf('rule:l-1')).toBe('rule');
    expect(sourceOf('extractor')).toBe('extractor');
    expect(sourceOf('copied')).toBe('copied');
    expect(sourceOf('classifier')).toBe('classifier');
    expect(sourceOf('llm')).toBe('llm');
  });
});

describe('bulkApply', () => {
  it('sends a filter selection with the count the operator confirmed, and reads a changed count', async () => {
    const sent: unknown[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        sent.push(typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined);
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'count_changed', count: 25 }), { status: 409 }),
        );
      }),
    );
    const filter: DocumentsFilter = {
      ...EMPTY_FILTER,
      include: { ...EMPTY_FILTER.include, type: ['t1'] },
      search: 'nota',
    };
    const diff = { set: [{ field: 'tag', value_key: 'g1' }], remove: [] };
    await expect(
      bulkApply({ kind: 'filter', filter, exclude: ['d9'], expectedCount: 24 }, diff),
    ).rejects.toEqual(new CountChangedError(25));
    expect(sent[0]).toEqual({
      selection: {
        filter: {
          type: ['t1'],
          counterparty: [],
          tags: [],
          type_not: [],
          counterparty_not: [],
          tags_not: [],
          unassigned: [],
          tags_mode: 'any',
          date_from: '',
          date_to: '',
          q: 'nota',
          include_unreviewed: true,
          needs_review: false,
          file_type: 'documents',
          search_mode: 'content',
          inbox: false,
          custom: [],
        },
        exclude: ['d9'],
        expected_count: 24,
      },
      diff,
    });
    vi.unstubAllGlobals();
  });
});

it('keeps an equal-sized media selection on the same GET and bulk filter', async () => {
  let sent: unknown;
  vi.stubGlobal(
    'fetch',
    vi.fn((_url: unknown, init?: RequestInit) => {
      sent = JSON.parse(typeof init?.body === 'string' ? init.body : '{}');
      return Promise.resolve(new Response(JSON.stringify({ applied: 2 })));
    }),
  );
  const filter = {
    ...EMPTY_FILTER,
    fileType: 'video' as const,
    searchMode: 'content' as const,
    inbox: true,
    custom: [{ field: 'custom:invoice', operator: 'eq' as const, value: 'Cedar' }],
  };
  const query = new URLSearchParams(documentsQuery(filter, { sort: '', offset: 0, limit: 50 }));
  await bulkApply(
    { kind: 'filter', filter, exclude: [], expectedCount: 2 },
    { set: [{ field: 'tag', value_key: 'g1' }], remove: [] },
  );
  expect(query.get('file_type')).toBe('video');
  expect(query.get('inbox')).toBe('true');
  expect(JSON.parse(query.get('custom') ?? 'null')).toEqual(filter.custom);
  expect(sent).toMatchObject({
    selection: {
      expected_count: 2,
      filter: { file_type: 'video', search_mode: 'content', inbox: true, custom: filter.custom },
    },
  });
  vi.unstubAllGlobals();
});

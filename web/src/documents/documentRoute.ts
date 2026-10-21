import {
  documentsQuery,
  EMPTY_FILTER,
  FILE_TYPES,
  FILTER_FIELDS,
  type DocumentsFilter,
  type DocumentsPageRequest,
  type CustomFilter,
} from './documentsApi';
import type { DocumentDisplay, SavedView } from './workspaceApi';

export interface DocumentQuery {
  readonly filter: DocumentsFilter;
  readonly page: DocumentsPageRequest;
  readonly display: DocumentDisplay;
}

export const DOCUMENT_DESTINATIONS = [
  'overview',
  'library',
  'inbox',
  'processing',
  'views',
  'lists',
  'files',
] as const;

function customFilters(raw: string | null): readonly CustomFilter[] {
  try {
    const value: unknown = JSON.parse(raw ?? '[]');
    if (!Array.isArray(value)) return [];
    return value
      .filter((item: unknown): item is CustomFilter => {
        if (!item || typeof item !== 'object') return false;
        const field = item as Record<string, unknown>;
        return (
          typeof field.field === 'string' &&
          field.field.startsWith('custom:') &&
          (field.operator === 'unassigned' ||
            (field.operator === 'eq' && typeof field.value === 'string'))
        );
      })
      .map((item) =>
        item.operator === 'unassigned' ? { field: item.field, operator: item.operator } : item,
      );
  } catch {
    return [];
  }
}

export function readDocumentQuery(search: string): DocumentQuery {
  const params = new URLSearchParams(search);
  const kind = params.get('file_type');
  const offset = Number(params.get('offset') ?? 0);
  const limit = Number(params.get('limit') ?? 50);
  const display = params.get('display');
  const perField = (suffix: string) => ({
    type: params.getAll(`type${suffix}`),
    counterparty: params.getAll(`counterparty${suffix}`),
    tags: params.getAll(`tags${suffix}`),
  });
  return {
    filter: {
      ...EMPTY_FILTER,
      fileType: FILE_TYPES.find((value) => value === kind) ?? 'documents',
      searchMode: params.get('search_mode') === 'name' ? 'name' : 'content',
      search: params.get('q') ?? '',
      inbox: params.get('inbox') === 'true',
      needsReview: params.get('needs_review') === 'true',
      includeUnreviewed: params.get('include_unreviewed') !== 'false',
      tagsMode: params.get('tags_mode') === 'all' ? 'all' : 'any',
      dateFrom: params.get('date_from') ?? '',
      dateTo: params.get('date_to') ?? '',
      include: perField(''),
      exclude: perField('_not'),
      unassigned: FILTER_FIELDS.filter((field) => params.getAll('unassigned').includes(field)),
      custom: customFilters(params.get('custom')),
    },
    page: {
      sort: params.get('sort') ?? '-indexed_at',
      offset: Number.isSafeInteger(offset) && offset >= 0 ? offset : 0,
      limit: [25, 50, 100].includes(limit) ? limit : 50,
    },
    display: display === 'cards' || display === 'compact' ? display : 'table',
  };
}

export function writeDocumentQuery(query: DocumentQuery): string {
  const params = new URLSearchParams(documentsQuery(query.filter, query.page));
  params.set('display', query.display);
  return params.toString();
}

function resultPath(path: string | null): string {
  return path === '/documents/inbox' ? path : '/documents/library';
}

export function documentHref(id: string, from: string, query: DocumentQuery): string {
  const params = new URLSearchParams(writeDocumentQuery(query));
  params.set('from', resultPath(from));
  return `/documents/document/${encodeURIComponent(id)}?${params.toString()}`;
}

export function resultHref(params: URLSearchParams): string {
  return `${resultPath(params.get('from'))}?${writeDocumentQuery(readDocumentQuery(params.toString()))}`;
}

export function savedViewHref(view: SavedView): string {
  const query = readDocumentQuery(view.query);
  return `/documents/${query.filter.inbox ? 'inbox' : 'library'}?${writeDocumentQuery({ ...query, page: { ...query.page, offset: 0 }, display: view.display })}`;
}

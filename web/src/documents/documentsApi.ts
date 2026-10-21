import { getJSON, HttpError, httpErrorFrom, sendJSON } from '@/api/json';

/**
 * The Documenti view's client (internal/agui/documents_api.go): the library with the fields the
 * operator keeps on each document, and the lists those fields take their values from. Filters
 * carry label ids; the view resolves them to names through the lists.
 */
export const documentsBase = '/api/documents';

/** The three fields a filter picks values for. Its values sit on a document as "type",
 * "counterparty" and "tag". */
export type FilterField = 'type' | 'counterparty' | 'tags';

export const FILTER_FIELDS: readonly FilterField[] = ['type', 'counterparty', 'tags'];

/** One value on a document, with who set it (docfields.Value). */
export interface FieldValue {
  readonly field: string;
  readonly value_key: string;
  readonly value?: unknown;
  readonly set_by: string;
  readonly accepted: boolean;
  readonly evidence?: string;
  readonly set_at: string;
}

export interface DocumentRow {
  readonly id: string;
  readonly source_key: string;
  readonly file_name: string;
  readonly file_type?: FileType | 'other';
  readonly size_bytes: number;
  readonly indexed_at: string;
  readonly raw_sha256?: string;
  readonly record_id?: string;
  readonly values: readonly FieldValue[];
  readonly reviewed_at?: string;
  readonly needs_review: boolean;
}

/** A count per value of each field in the current result, plus how many documents leave each
 * field unassigned. */
export type Facets = Readonly<Record<string, Readonly<Record<string, number>>>>;

export interface DocumentsPage {
  readonly rows: readonly DocumentRow[];
  readonly total: number;
  readonly facets: Facets;
}

export type LabelKind = 'type' | 'counterparty' | 'tag';

export interface Label {
  readonly id: string;
  readonly kind: LabelKind;
  readonly name: string;
  readonly match_algorithm: string;
  readonly match_pattern: string;
  readonly case_insensitive: boolean;
}

export interface CustomField {
  readonly id: string;
  readonly name: string;
  readonly data_type: 'date' | 'monetary' | 'integer' | 'text' | 'select';
  readonly options?: readonly string[];
}

type PerField = Readonly<Record<FilterField, readonly string[]>>;

export const FILE_TYPES = [
  'documents',
  'pdf',
  'word',
  'spreadsheet',
  'presentation',
  'text',
  'image',
  'video',
  'audio',
  'code',
  'all',
] as const;
export type FileType = (typeof FILE_TYPES)[number];
export interface CustomFilter {
  readonly field: string;
  readonly operator: 'eq' | 'unassigned';
  readonly value?: string;
}

/** The filter bar's state. include and exclude hold label ids per field; unassigned names the
 * fields that must be empty. */
export interface DocumentsFilter {
  readonly fileType: FileType;
  readonly searchMode: 'name' | 'content';
  readonly inbox: boolean;
  readonly custom: readonly CustomFilter[];
  readonly include: PerField;
  readonly exclude: PerField;
  readonly unassigned: readonly FilterField[];
  readonly tagsMode: 'any' | 'all';
  readonly dateFrom: string;
  readonly dateTo: string;
  readonly search: string;
  readonly needsReview: boolean;
  /** False keeps only the values the operator set or accepted. */
  readonly includeUnreviewed: boolean;
}

const NO_VALUES: PerField = { type: [], counterparty: [], tags: [] };

export const EMPTY_FILTER: DocumentsFilter = {
  fileType: 'documents',
  searchMode: 'content',
  inbox: false,
  custom: [],
  include: NO_VALUES,
  exclude: NO_VALUES,
  unassigned: [],
  tagsMode: 'any',
  dateFrom: '',
  dateTo: '',
  search: '',
  needsReview: false,
  includeUnreviewed: true,
};

export interface DocumentsPageRequest {
  readonly sort: string;
  readonly offset: number;
  readonly limit: number;
}

/** documentsQuery spells a filter and a page the way GET /api/documents reads them. */
export function documentsQuery(filter: DocumentsFilter, page: DocumentsPageRequest): string {
  const params = new URLSearchParams();
  for (const field of FILTER_FIELDS) {
    for (const id of filter.include[field]) params.append(field, id);
    for (const id of filter.exclude[field]) params.append(`${field}_not`, id);
  }
  for (const field of filter.unassigned) params.append('unassigned', field);
  if (filter.tagsMode === 'all') params.set('tags_mode', 'all');
  if (filter.dateFrom !== '') params.set('date_from', filter.dateFrom);
  if (filter.dateTo !== '') params.set('date_to', filter.dateTo);
  if (filter.search.trim() !== '') params.set('q', filter.search.trim());
  if (filter.needsReview) params.set('needs_review', 'true');
  if (!filter.includeUnreviewed) params.set('include_unreviewed', 'false');
  params.set('file_type', filter.fileType);
  params.set('search_mode', filter.searchMode);
  if (filter.inbox) params.set('inbox', 'true');
  if (filter.custom.length) params.set('custom', JSON.stringify(filter.custom));
  if (page.sort !== '') params.set('sort', page.sort);
  params.set('offset', String(page.offset));
  params.set('limit', String(page.limit));
  return params.toString();
}

/** withField adds id to one side of a field, taking it out of the other, or takes it out when it
 * is already there: a value is included, excluded or neither. */
export function withField(
  filter: DocumentsFilter,
  side: 'include' | 'exclude',
  field: FilterField,
  id: string,
): DocumentsFilter {
  const other = side === 'include' ? 'exclude' : 'include';
  const present = filter[side][field].includes(id);
  return {
    ...filter,
    [side]: {
      ...filter[side],
      [field]: present ? filter[side][field].filter((v) => v !== id) : [...filter[side][field], id],
    },
    [other]: { ...filter[other], [field]: filter[other][field].filter((v) => v !== id) },
  };
}

/** A value named without its provenance: what a diff sets or removes (docfields.ValueRef). */
export interface DiffValue {
  readonly field: string;
  readonly value_key: string;
}

/** The change one write makes to documents (docfields.Diff): every value is the operator's. */
export interface FieldsDiff {
  readonly set: readonly DiffValue[];
  readonly remove: readonly DiffValue[];
}

export function isEmptyDiff(diff: FieldsDiff): boolean {
  return diff.set.length === 0 && diff.remove.length === 0;
}

/** One entry of a document's history (docfields.Event). */
export interface HistoryEvent {
  readonly field: string;
  readonly value_key: string;
  readonly action: 'set' | 'accepted' | 'rejected' | 'removed' | 'reviewed' | 'moved';
  readonly actor: string;
  readonly before?: FieldValue;
  readonly after?: FieldValue;
  readonly at: string;
}

/** One document as its detail reads it: its values, the suggestions the operator rejected, and
 * its history. */
export interface DocumentDetail extends DocumentRow {
  readonly rejections: readonly DiffValue[];
  readonly history: readonly HistoryEvent[];
}

export type ValueSource = 'rule' | 'extractor' | 'copied' | 'classifier' | 'llm';

/** sourceOf names who set a value the operator did not: the mark beside it in the list. */
export function sourceOf(setBy: string): ValueSource | null {
  if (setBy === 'operator') return null;
  if (setBy.startsWith('rule:')) return 'rule';
  if (setBy === 'extractor' || setBy === 'copied' || setBy === 'classifier' || setBy === 'llm') {
    return setBy;
  }
  return null;
}

export function fetchDocuments(
  filter: DocumentsFilter,
  page: DocumentsPageRequest,
  signal?: AbortSignal,
): Promise<DocumentsPage> {
  return getJSON<DocumentsPage>(`${documentsBase}?${documentsQuery(filter, page)}`, signal);
}

/** fetchReviewCount is how many documents of the whole library wait for review. */
export async function fetchReviewCount(signal?: AbortSignal): Promise<number> {
  const page = await getJSON<DocumentsPage>(`${documentsBase}?needs_review=true&limit=1`, signal);
  return page.total;
}

export async function fetchLabels(
  kind: LabelKind,
  signal?: AbortSignal,
): Promise<readonly Label[]> {
  const out = await getJSON<{ labels: readonly Label[] }>(
    `${documentsBase}/labels?kind=${encodeURIComponent(kind)}`,
    signal,
  );
  return out.labels;
}

export async function fetchCustomFields(signal?: AbortSignal): Promise<readonly CustomField[]> {
  const out = await getJSON<{ fields: readonly CustomField[] }>(
    `${documentsBase}/custom-fields`,
    signal,
  );
  return out.fields;
}

/** filterBody spells a filter the way a bulk selection's body carries it
 * (internal/agui/documents_api.go documentsFilter). */
export function filterBody(filter: DocumentsFilter): Record<string, unknown> {
  return {
    type: filter.include.type,
    counterparty: filter.include.counterparty,
    tags: filter.include.tags,
    type_not: filter.exclude.type,
    counterparty_not: filter.exclude.counterparty,
    tags_not: filter.exclude.tags,
    unassigned: filter.unassigned,
    tags_mode: filter.tagsMode,
    date_from: filter.dateFrom,
    date_to: filter.dateTo,
    q: filter.search.trim(),
    include_unreviewed: filter.includeUnreviewed,
    needs_review: filter.needsReview,
    file_type: filter.fileType,
    search_mode: filter.searchMode,
    inbox: filter.inbox,
    custom: filter.custom,
  };
}

/** What a bulk edit applies to: the documents ticked, or every document of a filter minus the
 * ones unticked, with the count the operator confirmed. */
export type BulkSelection =
  | { readonly kind: 'ids'; readonly ids: readonly string[] }
  | {
      readonly kind: 'filter';
      readonly filter: DocumentsFilter;
      readonly exclude: readonly string[];
      readonly expectedCount: number;
    };

/** CountChangedError is a filter that no longer holds the count the operator confirmed:
 * nothing was written, and count is what it holds now. */
export class CountChangedError extends Error {
  readonly count: number;

  constructor(count: number) {
    super(`the filter now holds ${String(count)} documents`);
    this.name = 'CountChangedError';
    this.count = count;
  }
}

/** bulkApply sends one diff to a selection. A 409 is a changed count, thrown as CountChangedError
 * so the view can ask again. */
export async function bulkApply(selection: BulkSelection, diff: FieldsDiff): Promise<number> {
  const body =
    selection.kind === 'ids'
      ? { selection: { ids: selection.ids }, diff }
      : {
          selection: {
            filter: filterBody(selection.filter),
            exclude: selection.exclude,
            expected_count: selection.expectedCount,
          },
          diff,
        };
  const res = await fetch(`${documentsBase}/bulk`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
  });
  if (res.status === 409) {
    const conflict = (await res.json()) as { count: number };
    throw new CountChangedError(conflict.count);
  }
  if (!res.ok) throw await httpErrorFrom(res);
  const out = (await res.json()) as { applied: number };
  return out.applied;
}

const documentURL = (id: string) => `${documentsBase}/${encodeURIComponent(id)}`;

export function fetchDocument(id: string, signal?: AbortSignal): Promise<DocumentDetail> {
  return getJSON<DocumentDetail>(documentURL(id), signal);
}

/** patchDocument writes one document's diff. Removing a value the operator did not set rejects
 * it: the server records the rejection and never suggests the value again. */
export async function patchDocument(id: string, diff: FieldsDiff): Promise<void> {
  await sendJSON('PATCH', documentURL(id), diff);
}

/** reviewDocument marks a document reviewed: every suggestion left on it counts as accepted. */
export async function reviewDocument(id: string): Promise<void> {
  await sendJSON('POST', `${documentURL(id)}/review`);
}

/** fetchNextToReview names the first document of the library still waiting for review, other
 * than the one just left, or null when none is. */
export async function fetchNextToReview(after: string): Promise<string | null> {
  const page = await getJSON<DocumentsPage>(`${documentsBase}?needs_review=true&limit=2`);
  return page.rows.find((row) => row.id !== after)?.id ?? null;
}

/** A label as the operator writes it: no id for a new one. */
export type LabelInput = Omit<Label, 'id'>;

/** saveLabel creates a label, or updates the one id names. A label with a rule runs it over the
 * library before the answer, which says how many documents it suggested. */
export function saveLabel(
  label: LabelInput,
  id?: string,
): Promise<{ readonly label: Label; readonly suggested: number }> {
  return id === undefined
    ? sendJSON('POST', `${documentsBase}/labels`, label)
    : sendJSON('PUT', `${documentsBase}/labels/${encodeURIComponent(id)}`, label);
}

/** deleteLabel removes a label; the values naming it read as an unknown label. */
export async function deleteLabel(id: string): Promise<void> {
  await sendJSON('DELETE', `${documentsBase}/labels/${encodeURIComponent(id)}`);
}

export type CustomFieldInput = Omit<CustomField, 'id'>;

export function saveCustomField(field: CustomFieldInput, id?: string): Promise<CustomField> {
  return id === undefined
    ? sendJSON('POST', `${documentsBase}/custom-fields`, field)
    : sendJSON('PUT', `${documentsBase}/custom-fields/${encodeURIComponent(id)}`, field);
}

export async function deleteCustomField(id: string): Promise<void> {
  await sendJSON('DELETE', `${documentsBase}/custom-fields/${encodeURIComponent(id)}`);
}

/** errorMessage is what the operator reads of a failed write: the server's own sentence when it
 * sent one (writeDocumentsError puts it beside the code). */
export function errorMessage(err: unknown): string {
  if (err instanceof HttpError && err.detail !== '') return err.detail;
  return err instanceof Error ? err.message : String(err);
}

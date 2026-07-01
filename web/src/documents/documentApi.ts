export type DocumentScope = 'thread' | 'library';
export type DocumentStatus =
  | 'draft'
  | 'queued'
  | 'processing'
  | 'ready'
  | 'failed'
  | 'deleting'
  | 'deleted'
  | 'archived';

export interface DocumentItem {
  readonly id: string;
  readonly identity_id: string;
  readonly scope: DocumentScope;
  readonly title: string;
  readonly tags: readonly string[];
  readonly metadata: Record<string, unknown>;
  readonly active_version_id?: string;
  readonly status: DocumentStatus;
  readonly created_at?: string;
  readonly updated_at?: string;
  readonly deleted_at?: string;
  // Denormalized active-version facts on list rows so each row shows a size + kind
  // without an N+1 detail fetch (populated by GET /api/documents only).
  readonly active_size_bytes?: number;
  readonly active_content_type?: string;
}

type RawDocumentItem = Omit<DocumentItem, 'tags'> & {
  readonly tags?: readonly string[] | null;
};

type RawDocumentDetail = Omit<DocumentDetail, 'document'> & {
  readonly document: RawDocumentItem;
};

export interface DocumentVersion {
  readonly id: string;
  readonly document_id: string;
  readonly asset_id?: string;
  readonly version_number: number;
  readonly status: string;
  readonly sha1?: string;
  readonly sha256: string;
  readonly content_type: string;
  readonly size_bytes: number;
  readonly storage_object_id: string;
  readonly chunking_config_hash?: string;
  readonly pipeline_config_hash?: string;
  readonly created_at?: string;
  readonly updated_at?: string;
}

export interface DocumentDetail {
  readonly document: DocumentItem;
  readonly versions: readonly DocumentVersion[];
}

export interface IngestionEvent {
  readonly id: number;
  readonly entity_type: string;
  readonly entity_id: string;
  readonly job_id?: string;
  readonly from_status?: string;
  readonly to_status?: string;
  readonly event_type: string;
  readonly message?: string;
  readonly detail: Record<string, unknown>;
  readonly trace_id?: string;
  readonly created_at?: string;
}

export interface ListDocumentsParams {
  readonly query?: string;
  readonly tag?: string;
  readonly scope?: DocumentScope;
  readonly limit?: number;
  readonly offset?: number;
}

export interface UpdateDocumentInput {
  readonly title?: string;
  readonly tags?: readonly string[];
  readonly scope?: DocumentScope;
  readonly status?: DocumentStatus;
  readonly active_version_id?: string;
  readonly metadata?: Record<string, unknown>;
}

export interface StorageObjectInfo {
  readonly Ref?: {
    readonly Bucket?: string;
    readonly Key?: string;
  };
  readonly Attrs?: {
    readonly SizeBytes?: number;
    readonly ETag?: string;
    readonly MIMEType?: string;
  };
}

export interface StorageOrphanReport {
  readonly bucket: string;
  readonly prefix: string;
  readonly dry_run_token: string;
  readonly orphans: readonly StorageObjectInfo[];
  readonly deleted: number;
}

export interface StorageOrphanParams {
  readonly bucket: string;
  readonly prefix?: string;
}

export interface StorageOrphanCleanupInput extends StorageOrphanParams {
  readonly dry_run_token: string;
  readonly confirm: 'DELETE ORPHAN OBJECTS';
}

async function readJSON<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  return (await res.json()) as T;
}

function normalizeDocumentItem(item: RawDocumentItem): DocumentItem {
  return { ...item, tags: Array.isArray(item.tags) ? item.tags : [] };
}

function normalizeDocumentDetail(detail: RawDocumentDetail): DocumentDetail {
  return { ...detail, document: normalizeDocumentItem(detail.document) };
}

function appendParam(params: URLSearchParams, key: string, value: string | number | undefined) {
  if (value === undefined) return;
  if (typeof value === 'string' && value.trim() === '') return;
  params.set(key, String(value));
}

function queryString(params: URLSearchParams): string {
  const encoded = params.toString();
  return encoded.length === 0 ? '' : `?${encoded}`;
}

export async function fetchDocuments(params: ListDocumentsParams = {}): Promise<DocumentItem[]> {
  const qs = new URLSearchParams();
  appendParam(qs, 'scope', params.scope);
  appendParam(qs, 'q', params.query);
  appendParam(qs, 'tag', params.tag);
  appendParam(qs, 'limit', params.limit);
  appendParam(qs, 'offset', params.offset);
  const res = await fetch(`/api/documents${queryString(qs)}`, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  const items = await readJSON<RawDocumentItem[]>(res);
  return items.map(normalizeDocumentItem);
}

export async function fetchDocumentDetail(id: string): Promise<DocumentDetail> {
  const res = await fetch(`/api/documents/${encodeURIComponent(id)}`, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return normalizeDocumentDetail(await readJSON<RawDocumentDetail>(res));
}

export async function fetchDocumentEvents(id: string): Promise<IngestionEvent[]> {
  const res = await fetch(`/api/documents/${encodeURIComponent(id)}/events`, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<IngestionEvent[]>(res);
}

export async function updateDocument(
  id: string,
  input: UpdateDocumentInput,
): Promise<DocumentItem> {
  const res = await fetch(`/api/documents/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(input),
  });
  return normalizeDocumentItem(await readJSON<RawDocumentItem>(res));
}

export async function deleteDocument(id: string): Promise<DocumentItem> {
  const res = await fetch(`/api/documents/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return normalizeDocumentItem(await readJSON<RawDocumentItem>(res));
}

export async function retryDocumentAsset(assetId: string): Promise<unknown> {
  const res = await fetch(`/api/assets/${encodeURIComponent(assetId)}/retry`, {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<unknown>(res);
}

export async function fetchStorageOrphans(
  params: StorageOrphanParams,
): Promise<StorageOrphanReport> {
  const qs = new URLSearchParams();
  appendParam(qs, 'bucket', params.bucket);
  appendParam(qs, 'prefix', params.prefix);
  const res = await fetch(`/api/storage/orphans${queryString(qs)}`, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<StorageOrphanReport>(res);
}

export async function cleanupStorageOrphans(
  input: StorageOrphanCleanupInput,
): Promise<StorageOrphanReport> {
  const res = await fetch('/api/storage/orphans/cleanup', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(input),
  });
  return readJSON<StorageOrphanReport>(res);
}

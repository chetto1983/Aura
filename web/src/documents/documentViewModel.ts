import type { DocumentDetail, DocumentItem, DocumentStatus, DocumentVersion } from './documentApi';

export type DocumentTab = 'all' | 'documents' | 'images' | 'files' | 'failed' | 'processing';
export type DocumentKind = 'document' | 'image' | 'file';
export type StatusTone = 'success' | 'danger' | 'warning' | 'secondary';

const imageTypes = new Set(['image/png', 'image/jpeg', 'image/webp', 'image/gif', 'image/svg+xml']);
const documentTypes = new Set([
  'application/pdf',
  'application/msword',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/vnd.ms-excel',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  'application/vnd.ms-powerpoint',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  'text/plain',
  'text/markdown',
  'text/csv',
  'text/html',
]);

export function activeVersionFor(detail: DocumentDetail | undefined): DocumentVersion | undefined {
  if (detail === undefined) return undefined;
  return (
    detail.versions.find((version) => version.id === detail.document.active_version_id) ??
    detail.versions[0]
  );
}

export function documentKindFor(
  document: DocumentItem,
  version: DocumentVersion | undefined,
): DocumentKind {
  const mime = (version?.content_type ?? document.active_content_type ?? '').toLowerCase();
  if (imageTypes.has(mime) || /\.(png|jpe?g|webp|gif|svg)$/i.test(document.title)) return 'image';
  if (
    documentTypes.has(mime) ||
    /\.(pdf|docx?|xlsx?|pptx?|txt|md|csv|html)$/i.test(document.title)
  ) {
    return 'document';
  }
  return 'file';
}

export function documentMatchesTab(
  document: DocumentItem,
  version: DocumentVersion | undefined,
  tab: DocumentTab,
): boolean {
  if (tab === 'all') return true;
  if (tab === 'failed') return failedStatuses.has(document.status);
  if (tab === 'processing') return inFlightStatuses.has(document.status);
  const kind = documentKindFor(document, version);
  if (tab === 'documents') return kind === 'document';
  if (tab === 'images') return kind === 'image';
  return kind === 'file';
}

// Statuses aura.documents_status_check treats as a failed pipeline attempt. failed can
// still be retried; dead_letter means retries are exhausted. Both the failed tab and the
// retry action in DocumentActionMenu key off this set so they cannot drift apart again.
export const failedStatuses = new Set<DocumentStatus>(['failed', 'dead_letter']);

// The pipeline stages a document passes through between acceptance and ready. The tab
// is named for the state the operator cares about, not for any single status value.
// 'stored' is the only one of these the pipeline actually reaches today: RecordAssetVersion
// (and the wrapper that calls it) run from inside the asset processing queue, so a job is
// already in flight by the time a document is written to 'stored'. The remaining five --
// queued, converting, chunking, embedding, projecting -- are not yet written by any code
// path; the pipeline currently jumps stored -> ready. They stay in the set because they
// are legal values under aura.documents_status_check and name the lifecycle it
// anticipates, so a future reader must not "clean up" the unreachable ones.
const inFlightStatuses = new Set<DocumentStatus>([
  'stored',
  'queued',
  'converting',
  'chunking',
  'embedding',
  'projecting',
]);

export function statusToneFor(status: DocumentStatus): StatusTone {
  if (status === 'ready') return 'success';
  if (failedStatuses.has(status) || status === 'deleted') return 'danger';
  if (inFlightStatuses.has(status) || status === 'deleting') return 'warning';
  return 'secondary';
}

export function parseDocumentTags(value: string): string[] {
  const seen = new Set<string>();
  const tags: string[] = [];
  for (const raw of value.split(',')) {
    const tag = raw.trim();
    if (tag.length === 0 || seen.has(tag)) continue;
    seen.add(tag);
    tags.push(tag);
  }
  return tags;
}

export function formatDocumentDate(value: string | undefined): string {
  if (value === undefined || value.trim() === '') return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
  }).format(date);
}

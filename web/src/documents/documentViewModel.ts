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
  if (tab === 'failed') return document.status === 'failed';
  if (tab === 'processing') return document.status === 'queued' || document.status === 'processing';
  const kind = documentKindFor(document, version);
  if (tab === 'documents') return kind === 'document';
  if (tab === 'images') return kind === 'image';
  return kind === 'file';
}

export function statusToneFor(status: DocumentStatus): StatusTone {
  if (status === 'ready') return 'success';
  if (status === 'failed' || status === 'deleted') return 'danger';
  if (status === 'queued' || status === 'processing' || status === 'deleting') return 'warning';
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

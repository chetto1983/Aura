export const REDACTED_ARG_VALUE = '[redacted]';

export type ToolStatusTone = 'running' | 'done' | 'error' | 'action';

export type WebSearchResult = {
  title: string;
  url?: string;
  snippet?: string;
};

export type WikiPageSummary = {
  slug: string;
  title: string;
  preview?: string;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function parseJSONRecord(value: unknown): Record<string, unknown> | null {
  if (isRecord(value)) return value;
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (!trimmed.startsWith('{')) return null;
  try {
    const parsed = JSON.parse(trimmed);
    return isRecord(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

export function safeJSONStringify(value: unknown): string {
  if (value === undefined) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

export function getArgKeys(args?: unknown, argsText?: string): string[] {
  const record = parseJSONRecord(args) ?? parseJSONRecord(argsText);
  if (!record) return [];
  return Object.keys(record).filter((key) => key.trim() !== '').sort();
}

export function redactArgsForDisplay(args?: unknown, argsText?: string): Record<string, string> {
  return getArgKeys(args, argsText).reduce<Record<string, string>>((acc, key) => {
    acc[key] = REDACTED_ARG_VALUE;
    return acc;
  }, {});
}

export function summarizeArgKeys(args?: unknown, argsText?: string, maxKeys = 4): string {
  const keys = getArgKeys(args, argsText);
  if (keys.length === 0) return '';
  const shown = keys.slice(0, maxKeys).join(', ');
  const remaining = keys.length - maxKeys;
  return remaining > 0 ? `${shown} +${remaining}` : shown;
}

export function toolStatusTone(statusType?: string, reason?: string): ToolStatusTone {
  if (statusType === 'running') return 'running';
  if (statusType === 'requires-action') return 'action';
  if (statusType === 'incomplete' || reason === 'error') return 'error';
  return 'done';
}

export function extractWikiPageSummary(result: unknown): WikiPageSummary | null {
  const record = parseJSONRecord(result);
  if (!record) return null;

  const page = isRecord(record.page) ? record.page : record;
  const slug = stringField(page, 'slug');
  if (!slug) return null;

  const title = stringField(page, 'title') || humanizeSlug(slug);
  const preview =
    stringField(page, 'preview') ||
    stringField(page, 'summary') ||
    firstBodyLine(stringField(page, 'body'));

  return { slug, title, preview };
}

export function extractWebSearchResults(result: unknown): WebSearchResult[] {
  const record = parseJSONRecord(result);
  if (!record) return [];

  const rawResults =
    arrayField(record, 'results') ||
    arrayField(record, 'items') ||
    (isRecord(record.result) ? arrayField(record.result, 'results') : null) ||
    [];

  return rawResults
    .filter(isRecord)
    .map((item) => ({
      title: stringField(item, 'title') || stringField(item, 'name') || stringField(item, 'url') || 'Result',
      url: stringField(item, 'url') || stringField(item, 'href'),
      snippet:
        stringField(item, 'snippet') ||
        stringField(item, 'content') ||
        stringField(item, 'description'),
    }))
    .filter((item) => item.title.trim() !== '');
}

export function resultPreview(result: unknown, maxLength = 280): string {
  const record = parseJSONRecord(result);
  const preview = record ? stringField(record, 'preview') : '';
  const raw = preview || (typeof result === 'string' ? result : safeJSONStringify(result));
  const normalized = raw.replace(/\s+/g, ' ').trim();
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, maxLength - 1)}...`;
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === 'string' ? value.trim() : '';
}

function arrayField(record: Record<string, unknown>, key: string): unknown[] | null {
  const value = record[key];
  return Array.isArray(value) ? value : null;
}

function humanizeSlug(slug: string): string {
  return slug
    .split(/[-_]+/)
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(' ');
}

function firstBodyLine(body: string): string {
  return body
    .split(/\r?\n/)
    .map((line) => line.replace(/^#+\s*/, '').trim())
    .find(Boolean) ?? '';
}

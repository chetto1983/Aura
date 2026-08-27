import type { Unstable_DirectiveFormatter } from '@assistant-ui/core';
import type { Unstable_TriggerItem } from '@assistant-ui/react';
import type { IEntity } from '@svar-ui/react-filemanager';

export type GarageScopeKind = 'file' | 'folder';

export interface GarageDocumentScope {
  readonly kind: GarageScopeKind;
  /** Garage object key, with no leading or trailing slash. */
  readonly path: string;
}

export type GarageFolderLister = (path: string) => Promise<readonly IEntity[]>;

export const GARAGE_TRIGGER_CHAR = '@';
export const GARAGE_FILE_ITEM_TYPE = 'garage-file';
export const GARAGE_FOLDER_ITEM_TYPE = 'garage-folder';

const GARAGE_MENTION = /(^|[\s([{])@(file|folder):("(?:\\.|[^"\\])*")(?=$|[\s)\]}.,;:!?])/g;

function normalizePath(value: string): string | undefined {
  const segments = value.trim().replaceAll('\\', '/').split('/');
  if (segments.some((segment) => segment === '.' || segment === '..')) return undefined;
  const path = segments.filter((segment) => segment.length > 0).join('/');
  return path.length === 0 ? undefined : path;
}

function displayPath(path: string): string {
  return `/${path}`;
}

function itemKind(item: Unstable_TriggerItem): GarageScopeKind | undefined {
  const kind = item.metadata?.kind;
  return kind === 'file' || kind === 'folder' ? kind : undefined;
}

function itemPath(item: Unstable_TriggerItem): string | undefined {
  const value = item.metadata?.path;
  return typeof value === 'string' ? normalizePath(value) : undefined;
}

export function serializeGarageMention(item: Unstable_TriggerItem): string {
  const kind = itemKind(item);
  const path = itemPath(item);
  if (kind === undefined || path === undefined) return `${GARAGE_TRIGGER_CHAR}${item.id}`;
  return `${GARAGE_TRIGGER_CHAR}${kind}:${JSON.stringify(displayPath(path))}`;
}

interface ParsedMention {
  readonly start: number;
  readonly end: number;
  readonly kind: GarageScopeKind;
  readonly path: string;
}

function parsedMentions(text: string): readonly ParsedMention[] {
  const mentions: ParsedMention[] = [];
  for (const match of text.matchAll(GARAGE_MENTION)) {
    const prefix = match[1] ?? '';
    const kind = match[2] as GarageScopeKind;
    const encodedPath = match[3];
    if (encodedPath === undefined) continue;
    let decoded: unknown;
    try {
      decoded = JSON.parse(encodedPath);
    } catch {
      continue;
    }
    if (typeof decoded !== 'string') continue;
    const path = normalizePath(decoded);
    if (path === undefined) continue;
    mentions.push({
      start: match.index + prefix.length,
      end: match.index + match[0].length,
      kind,
      path,
    });
  }
  return mentions;
}

/** Extracts the structural run scope while leaving the visible message untouched. */
export function resolveGarageMentions(text: string): readonly GarageDocumentScope[] {
  const scopes: GarageDocumentScope[] = [];
  const seen = new Set<string>();
  for (const mention of parsedMentions(text)) {
    const key = `${mention.kind}\0${mention.path}`;
    if (seen.has(key)) continue;
    seen.add(key);
    scopes.push({ kind: mention.kind, path: mention.path });
  }
  return scopes;
}

export function createGarageDirectiveFormatter(): Unstable_DirectiveFormatter {
  return {
    serialize: serializeGarageMention,
    parse: (text) => {
      const mentions = parsedMentions(text);
      if (mentions.length === 0) return [{ kind: 'text', text }];
      const segments = [];
      let cursor = 0;
      for (const mention of mentions) {
        if (mention.start > cursor) {
          segments.push({ kind: 'text' as const, text: text.slice(cursor, mention.start) });
        }
        segments.push({
          kind: 'mention' as const,
          type: mention.kind === 'file' ? GARAGE_FILE_ITEM_TYPE : GARAGE_FOLDER_ITEM_TYPE,
          label: displayPath(mention.path),
          id: `${mention.kind}:${mention.path}`,
        });
        cursor = mention.end;
      }
      if (cursor < text.length) segments.push({ kind: 'text' as const, text: text.slice(cursor) });
      return segments;
    },
  };
}

interface CompletionQuery {
  readonly kind?: GarageScopeKind;
  readonly folder: string;
  readonly needle: string;
}

function completionQuery(query: string): CompletionQuery | undefined {
  let value = query.trim().replaceAll('\\', '/');
  let kind: GarageScopeKind | undefined;
  const lower = value.toLowerCase();
  if (lower.startsWith('file:')) {
    kind = 'file';
    value = value.slice('file:'.length);
  } else if (lower.startsWith('folder:')) {
    kind = 'folder';
    value = value.slice('folder:'.length);
  }
  value = value.replace(/^\/+/, '');
  const parts = value.split('/');
  if (parts.some((part) => part === '.' || part === '..' || part.includes('\0'))) return undefined;
  const needle = parts.pop() ?? '';
  const parent = parts.filter((part) => part.length > 0).join('/');
  return {
    ...(kind === undefined ? {} : { kind }),
    folder: parent === '' ? '' : `/${parent}`,
    needle,
  };
}

function entityLabel(entry: IEntity): string {
  if (typeof entry.name === 'string' && entry.name.trim().length > 0) return entry.name.trim();
  const id = entry.id.replace(/\/$/, '');
  return id.slice(id.lastIndexOf('/') + 1);
}

function matchRank(entry: IEntity, needle: string): number {
  if (needle === '') return entry.type === 'folder' ? 0 : 1;
  const candidate = entityLabel(entry).toLowerCase();
  const path = entry.id.toLowerCase();
  if (candidate === needle) return 0;
  if (candidate.startsWith(needle)) return 1;
  if (candidate.includes(needle)) return 2;
  return path.includes(needle) ? 3 : -1;
}

function triggerItem(entry: IEntity): Unstable_TriggerItem | undefined {
  if (entry.type !== 'file' && entry.type !== 'folder') return undefined;
  const path = normalizePath(entry.id);
  if (path === undefined) return undefined;
  return {
    id: `${entry.type}:${path}`,
    type: entry.type === 'file' ? GARAGE_FILE_ITEM_TYPE : GARAGE_FOLDER_ITEM_TYPE,
    label: entityLabel(entry),
    description: displayPath(path),
    metadata: { kind: entry.type, path },
  };
}

/**
 * Builds the async assistant-ui completion source. A query containing a slash lists exactly
 * that parent page; it never recursively walks Garage to manufacture a global index.
 */
export function createGarageMentionFetcher(
  listFolder: GarageFolderLister,
): (query: string) => Promise<readonly Unstable_TriggerItem[]> {
  let cachedFolder: string | undefined;
  let cachedEntries: Promise<readonly IEntity[]> | undefined;

  const entriesFor = (folder: string) => {
    if (cachedFolder === folder && cachedEntries !== undefined) return cachedEntries;
    cachedFolder = folder;
    const pending = listFolder(folder).catch((error: unknown) => {
      if (cachedEntries === pending) {
        cachedFolder = undefined;
        cachedEntries = undefined;
      }
      throw error;
    });
    cachedEntries = pending;
    return pending;
  };

  return async (query) => {
    const parsed = completionQuery(query);
    if (parsed === undefined) return [];
    const needle = parsed.needle.toLowerCase();
    const ranked = (await entriesFor(parsed.folder))
      .filter((entry) => parsed.kind === undefined || entry.type === parsed.kind)
      .map((entry) => ({ entry, rank: matchRank(entry, needle) }))
      .filter((row) => row.rank >= 0)
      .sort(
        (left, right) =>
          left.rank - right.rank || entityLabel(left.entry).localeCompare(entityLabel(right.entry)),
      );
    return ranked.flatMap(({ entry }) => {
      const item = triggerItem(entry);
      return item === undefined ? [] : [item];
    });
  };
}

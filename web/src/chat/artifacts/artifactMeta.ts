import type { TFunction } from 'i18next';
import {
  Code2,
  File,
  FileCode,
  FileImage,
  FileSpreadsheet,
  FileText,
  Globe,
  type LucideIcon,
} from 'lucide-react';

// Pure artifact-meta helpers shared by the 37B "Artefatti" panel and the inline
// local_artifact chip. Everything here is decision logic only (no React, no DOM),
// so it tests to 100% and is a Stryker mutation target (RESEARCH Pitfall 4): the
// MIME→renderer gate (previewKind), the mime/extension→category label + lucide
// icon map (D-16), and the byte formatter moved out of LocalArtifactDisplay so the
// panel and the chip share one implementation (WEBART-08 refactor-on-touch, DRY).

/** How the preview modal should render an asset — or `download` when no safe
 *  in-browser renderer applies. `image/svg+xml` is deliberately `download` (never
 *  `image`) to keep script-bearing SVG out of an executing <img> (T-37B-05). */
export type PreviewKind = 'image' | 'pdf' | 'text' | 'html' | 'docx' | 'xlsx' | 'download';

// The raw-<pre> text family (D-07): rendered as escaped monospace, never as
// interpreted markdown/HTML — zero injection surface.
const TEXT_EXT = new Set([
  'txt',
  'md',
  'csv',
  'json',
  'log',
  'sh',
  'yaml',
  'yml',
  'xml',
  'ts',
  'js',
  'py',
  'go',
]);

/** The lowercase extension after the last dot, or '' when the name has no dot. */
function extOf(filename: string): string {
  const lower = filename.toLowerCase();
  const dot = lower.lastIndexOf('.');
  return dot >= 0 ? lower.slice(dot + 1) : '';
}

/** Route an asset to its preview renderer. SVG is gated to `download` FIRST — before
 *  the `image/*` branch — so a script-bearing SVG never reaches an executing <img>. */
export function previewKind(mime: string, filename: string): PreviewKind {
  const ext = extOf(filename);
  if (mime === 'image/svg+xml' || ext === 'svg') return 'download';
  if (mime.startsWith('image/')) return 'image';
  if (mime === 'application/pdf' || ext === 'pdf') return 'pdf';
  if (mime === 'text/html' || ext === 'html' || ext === 'htm') return 'html';
  if (ext === 'docx') return 'docx';
  if (ext === 'xlsx') return 'xlsx';
  if (mime.startsWith('text/') || TEXT_EXT.has(ext)) return 'text';
  return 'download';
}

// The human category behind a row's "Categoria · EXT" label + its icon. `file` is
// the fallback (unknown/binary): it has no ICON_FOR_CATEGORY entry, so categoryIcon
// returns the generic `File` glyph for it (the CitationBubble Partial+fallback shape).
type Category = 'document' | 'text' | 'spreadsheet' | 'image' | 'code' | 'data' | 'web' | 'file';

const EXT_CATEGORY: Record<string, Category> = {
  png: 'image',
  jpg: 'image',
  jpeg: 'image',
  gif: 'image',
  webp: 'image',
  bmp: 'image',
  svg: 'image',
  xls: 'spreadsheet',
  xlsx: 'spreadsheet',
  ods: 'spreadsheet',
  csv: 'spreadsheet',
  html: 'web',
  htm: 'web',
  pdf: 'document',
  doc: 'document',
  docx: 'document',
  odt: 'document',
  rtf: 'document',
  md: 'document',
  markdown: 'document',
  json: 'data',
  yaml: 'data',
  yml: 'data',
  xml: 'data',
  toml: 'data',
  sh: 'code',
  bash: 'code',
  go: 'code',
  py: 'code',
  ts: 'code',
  tsx: 'code',
  js: 'code',
  jsx: 'code',
  sql: 'code',
  txt: 'text',
  log: 'text',
};

// Mirrors the CitationBubble ICON_FOR_KIND precedent: a Partial map + a `File`
// fallback, so an unmapped category (`file`) resolves to the generic glyph.
const ICON_FOR_CATEGORY: Partial<Record<Category, LucideIcon>> = {
  document: FileText,
  text: FileText,
  spreadsheet: FileSpreadsheet,
  image: FileImage,
  code: Code2,
  data: FileCode,
  web: Globe,
};

/** Classify an asset for its label + icon. This is independent of previewKind: an
 *  SVG is `image` here (for labelling) but `download` for rendering. Extension wins;
 *  mime is the fallback for extension-less names. */
function category(mime: string, filename: string): Category {
  const byExt = EXT_CATEGORY[extOf(filename)];
  if (byExt !== undefined) return byExt;
  if (mime.startsWith('image/')) return 'image';
  if (mime === 'text/html') return 'web';
  if (mime.includes('spreadsheet')) return 'spreadsheet';
  if (mime === 'application/pdf' || mime.includes('word') || mime.includes('opendocument')) {
    return 'document';
  }
  if (mime.startsWith('text/')) return 'text';
  return 'file';
}

/** A localized "Categoria · EXT" row label, e.g. "Documento · MD" (D-16). The
 *  extension segment is dropped for a name with no extension. */
export function categoryLabel(mime: string, filename: string, t: TFunction): string {
  const word = t(`artifacts.category.${category(mime, filename)}`);
  const ext = extOf(filename);
  return ext.length > 0 ? `${word} · ${ext.toUpperCase()}` : word;
}

/** The lucide icon for an asset's category, with the generic `File` fallback. */
export function categoryIcon(mime: string, filename: string): LucideIcon {
  return ICON_FOR_CATEGORY[category(mime, filename)] ?? File;
}

const KB = 1024;
const MB = KB * 1024;
const GB = MB * 1024;

/** A compact human byte size via the i18n size keys (B / KB / MB / GB, 1 decimal).
 *  Moved verbatim from LocalArtifactDisplay so the panel + the chip share it. */
export function formatSize(bytes: number, t: TFunction): string {
  if (bytes < KB) return t('display.artifact.sizeBytes', { count: bytes });
  if (bytes < MB) return t('display.artifact.sizeKb', { value: (bytes / KB).toFixed(1) });
  if (bytes < GB) return t('display.artifact.sizeMb', { value: (bytes / MB).toFixed(1) });
  return t('display.artifact.sizeGb', { value: (bytes / GB).toFixed(1) });
}

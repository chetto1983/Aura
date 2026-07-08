import { describe, expect, it } from 'vitest';
import type { TFunction } from 'i18next';
import { Code2, File, FileCode, FileImage, FileSpreadsheet, FileText, Globe } from 'lucide-react';
import { categoryIcon, categoryLabel, formatSize, previewKind } from './artifactMeta';

// Pure artifact-meta helpers — exact-output assertions pinning the MIME→renderer
// gate (previewKind), the mime/extension→category label + lucide icon map (D-16),
// and the shared byte formatter. These are the Stryker mutation targets, so the
// assertions are deliberately literal (boundary + ordering sensitive).

// A stub translator that echoes the key so exact key selection is asserted without
// booting i18next; the second form appends the interpolation payload for formatSize.
const echoKey = ((key: string) => key) as unknown as TFunction;
const echoKeyOpts = ((key: string, opts?: Record<string, unknown>) =>
  `${key}|${JSON.stringify(opts ?? {})}`) as unknown as TFunction;

describe('previewKind', () => {
  it('gates SVG to download-only BEFORE the image branch (T-37B-05 XSS exclusion)', () => {
    expect(previewKind('image/svg+xml', 'x.svg')).toBe('download');
    expect(previewKind('', 'x.svg')).toBe('download');
    // The svg mime must win even when the extension looks like a raster image.
    expect(previewKind('image/svg+xml', 'x.png')).toBe('download');
  });

  it('routes raster images to the image renderer', () => {
    expect(previewKind('image/png', 'x.png')).toBe('image');
    expect(previewKind('image/webp', 'x.webp')).toBe('image');
  });

  it('routes pdf by mime and by extension fallback', () => {
    expect(previewKind('application/pdf', 'x.pdf')).toBe('pdf');
    expect(previewKind('application/octet-stream', 'x.pdf')).toBe('pdf');
  });

  it('routes html by mime and by the .html / .htm extension', () => {
    expect(previewKind('text/html', 'x.html')).toBe('html');
    expect(previewKind('', 'x.htm')).toBe('html');
  });

  it('routes docx and xlsx by extension', () => {
    expect(previewKind('', 'x.docx')).toBe('docx');
    expect(previewKind('', 'x.xlsx')).toBe('xlsx');
  });

  it('routes the text family by text/* mime and by known text/code extensions', () => {
    expect(previewKind('text/plain', 'x.txt')).toBe('text');
    for (const name of ['x.md', 'x.csv', 'x.json', 'x.sh', 'x.yaml', 'x.go']) {
      expect(previewKind('', name)).toBe('text');
    }
  });

  it('falls back to download for formats with no free renderer (pptx, binary)', () => {
    expect(previewKind('application/vnd.ms-powerpoint', 'x.pptx')).toBe('download');
    expect(previewKind('application/octet-stream', 'x.bin')).toBe('download');
  });
});

describe('categoryLabel', () => {
  it('builds "<category> · <UPPER-EXT>" from the mime/extension (D-16 examples)', () => {
    expect(categoryLabel('text/markdown', 'notes.md', echoKey)).toBe(
      'artifacts.category.document · MD',
    );
    expect(categoryLabel('', 'book.xlsx', echoKey)).toBe('artifacts.category.spreadsheet · XLSX');
    expect(categoryLabel('image/png', 'photo.png', echoKey)).toBe('artifacts.category.image · PNG');
    expect(categoryLabel('', 'deploy.sh', echoKey)).toBe('artifacts.category.code · SH');
  });

  it('classifies data, web, text, and unknown files by extension', () => {
    expect(categoryLabel('', 'config.json', echoKey)).toBe('artifacts.category.data · JSON');
    expect(categoryLabel('', 'page.html', echoKey)).toBe('artifacts.category.web · HTML');
    expect(categoryLabel('', 'server.log', echoKey)).toBe('artifacts.category.text · LOG');
    expect(categoryLabel('application/vnd.ms-powerpoint', 'deck.pptx', echoKey)).toBe(
      'artifacts.category.file · PPTX',
    );
  });

  it('omits the separator when the filename has no extension', () => {
    expect(categoryLabel('application/octet-stream', 'README', echoKey)).toBe(
      'artifacts.category.file',
    );
  });

  it('classifies by mime when the extension is unknown or absent', () => {
    expect(categoryLabel('image/avif', 'photo', echoKey)).toBe('artifacts.category.image');
    expect(categoryLabel('text/html', 'frame', echoKey)).toBe('artifacts.category.web');
    expect(
      categoryLabel(
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        'book',
        echoKey,
      ),
    ).toBe('artifacts.category.spreadsheet');
    expect(categoryLabel('application/pdf', 'doc', echoKey)).toBe('artifacts.category.document');
    expect(categoryLabel('application/msword', 'memo', echoKey)).toBe(
      'artifacts.category.document',
    );
    expect(categoryLabel('application/vnd.oasis.opendocument.text', 'sheet', echoKey)).toBe(
      'artifacts.category.document',
    );
    expect(categoryLabel('text/plain', 'notes', echoKey)).toBe('artifacts.category.text');
  });
});

describe('categoryIcon', () => {
  it('maps each category to its lucide icon', () => {
    expect(categoryIcon('', 'a.md')).toBe(FileText); // document
    expect(categoryIcon('', 'a.txt')).toBe(FileText); // text shares the document glyph
    expect(categoryIcon('', 'a.xlsx')).toBe(FileSpreadsheet);
    expect(categoryIcon('image/png', 'a.png')).toBe(FileImage);
    expect(categoryIcon('', 'a.sh')).toBe(Code2);
    expect(categoryIcon('', 'a.json')).toBe(FileCode); // data
    expect(categoryIcon('', 'a.html')).toBe(Globe); // web
  });

  it('falls back to the generic File icon for unknown/binary types', () => {
    expect(categoryIcon('application/octet-stream', 'a.bin')).toBe(File);
    expect(categoryIcon('application/vnd.ms-powerpoint', 'deck.pptx')).toBe(File);
  });
});

describe('formatSize', () => {
  it('selects the byte bucket below 1 KB', () => {
    expect(formatSize(0, echoKeyOpts)).toBe('display.artifact.sizeBytes|{"count":0}');
    expect(formatSize(512, echoKeyOpts)).toBe('display.artifact.sizeBytes|{"count":512}');
    expect(formatSize(1023, echoKeyOpts)).toBe('display.artifact.sizeBytes|{"count":1023}');
  });

  it('selects the KB bucket in [1 KB, 1 MB) with one decimal', () => {
    expect(formatSize(1024, echoKeyOpts)).toBe('display.artifact.sizeKb|{"value":"1.0"}');
    expect(formatSize(2048, echoKeyOpts)).toBe('display.artifact.sizeKb|{"value":"2.0"}');
  });

  it('selects the MB bucket in [1 MB, 1 GB), including the exact 1 MB boundary', () => {
    expect(formatSize(1024 * 1024, echoKeyOpts)).toBe('display.artifact.sizeMb|{"value":"1.0"}');
    expect(formatSize(5 * 1024 * 1024, echoKeyOpts)).toBe(
      'display.artifact.sizeMb|{"value":"5.0"}',
    );
  });

  it('selects the GB bucket at or above 1 GB', () => {
    expect(formatSize(3 * 1024 * 1024 * 1024, echoKeyOpts)).toBe(
      'display.artifact.sizeGb|{"value":"3.0"}',
    );
  });
});

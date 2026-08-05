import { describe, expect, it } from 'vitest';
import type { DocumentDetail, DocumentItem } from '../documentApi';
import {
  activeVersionFor,
  documentKindFor,
  documentMatchesTab,
  formatDocumentDate,
  parseDocumentTags,
  statusToneFor,
  type DocumentTab,
} from '../documentViewModel';

const doc: DocumentItem = {
  id: 'doc-1',
  identity_id: 'identity-1',
  scope: 'library',
  title: 'Manual.PDF',
  tags: ['robot'],
  metadata: {},
  active_version_id: 'ver-2',
  status: 'ready',
  updated_at: '2026-06-30T08:10:00Z',
};

const detail: DocumentDetail = {
  document: doc,
  versions: [
    {
      id: 'ver-1',
      document_id: 'doc-1',
      version_number: 1,
      status: 'ready',
      sha256: 'old',
      content_type: 'text/plain',
      size_bytes: 10,
      storage_object_id: 'store-1',
    },
    {
      id: 'ver-2',
      document_id: 'doc-1',
      version_number: 2,
      status: 'ready',
      sha256: 'new',
      content_type: 'application/pdf',
      size_bytes: 20,
      storage_object_id: 'store-2',
    },
  ],
};

describe('documentViewModel', () => {
  it('finds the active version and document kind', () => {
    expect(activeVersionFor(detail)?.id).toBe('ver-2');
    expect(documentKindFor(doc, activeVersionFor(detail))).toBe('document');
  });

  it.each([
    ['all', true],
    ['documents', true],
    ['images', false],
    ['failed', false],
    ['processing', false],
  ] satisfies [DocumentTab, boolean][])('matches %s tab', (tab, expected) => {
    expect(documentMatchesTab(doc, activeVersionFor(detail), tab)).toBe(expected);
  });

  it('maps status tones and tag drafts', () => {
    expect(statusToneFor('ready')).toBe('success');
    expect(statusToneFor('failed')).toBe('danger');
    expect(statusToneFor('dead_letter')).toBe('danger');
    expect(statusToneFor('converting')).toBe('warning');
    expect(statusToneFor('projecting')).toBe('warning');
    expect(statusToneFor('accepted')).toBe('secondary');
    expect(parseDocumentTags('robot, manual, robot')).toEqual(['robot', 'manual']);
  });

  it('treats every in-flight status as the processing tab', () => {
    for (const status of ['queued', 'converting', 'chunking', 'embedding', 'projecting'] as const) {
      expect(documentMatchesTab({ ...doc, status }, undefined, 'processing')).toBe(true);
    }
    expect(documentMatchesTab({ ...doc, status: 'ready' }, undefined, 'processing')).toBe(false);
  });

  it('formats dates compactly', () => {
    expect(formatDocumentDate('2026-06-30T08:10:00Z')).toMatch(/2026|Jun|30|giu|06/);
    expect(formatDocumentDate(undefined)).toBe('-');
  });
});

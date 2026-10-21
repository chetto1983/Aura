import { describe, expect, it } from 'vitest';
import type { DocumentRow } from './documentsApi';
import { bulkDiff, heldInFacets, heldInRows, valueState, type BulkPending } from './bulkModel';

function doc(id: string, values: readonly [string, string][]): DocumentRow {
  return {
    id,
    source_key: `chat/${id}`,
    file_name: id,
    size_bytes: 1,
    indexed_at: '',
    values: values.map(([field, key]) => ({
      field,
      value_key: key,
      set_by: 'operator',
      accepted: true,
      set_at: '',
    })),
    needs_review: false,
  };
}

const rows = [
  doc('d1', [
    ['counterparty', 'c-gialli'],
    ['tag', 'g-a'],
  ]),
  doc('d2', [
    ['counterparty', 'c-rossi'],
    ['tag', 'g-a'],
    ['tag', 'g-b'],
  ]),
  doc('d3', [['tag', 'g-a']]),
];

describe('valueState', () => {
  it('says whether a value is on all, some or none of the selection', () => {
    expect(valueState(rows, 'tags', 'g-a')).toEqual({ state: 'all', count: 3 });
    expect(valueState(rows, 'tags', 'g-b')).toEqual({ state: 'some', count: 1 });
    expect(valueState(rows, 'counterparty', 'c-verdi')).toEqual({ state: 'none', count: 0 });
  });
});

describe('bulkDiff', () => {
  it('sets and clears custom fields through the same explicit Diff contract', () => {
    const held = heldInRows([
      doc('custom', [
        ['custom:status', 'open'],
        ['custom:amount', '20'],
      ]),
    ]);
    expect(
      bulkDiff({ tags: {}, custom: { 'custom:status': 'paid', 'custom:amount': null } }, held),
    ).toEqual({
      set: [{ field: 'custom:status', value_key: 'paid' }],
      remove: [{ field: 'custom:amount', value_key: '20' }],
    });
  });
  it('sends only what the operator touched', () => {
    const pending: BulkPending = {
      counterparty: 'c-gialli',
      tags: { 'g-b': 'add', 'g-a': 'remove' },
    };
    expect(bulkDiff(pending, heldInRows(rows))).toEqual({
      set: [
        { field: 'counterparty', value_key: 'c-gialli' },
        { field: 'tag', value_key: 'g-b' },
      ],
      remove: [{ field: 'tag', value_key: 'g-a' }],
    });
  });

  it('clears a single field by removing every value the selection holds on it', () => {
    expect(bulkDiff({ counterparty: null, tags: {} }, heldInRows(rows))).toEqual({
      set: [],
      remove: [
        { field: 'counterparty', value_key: 'c-gialli' },
        { field: 'counterparty', value_key: 'c-rossi' },
      ],
    });
  });

  it('is empty when nothing was touched', () => {
    expect(bulkDiff({ tags: {} }, heldInRows(rows))).toEqual({ set: [], remove: [] });
  });

  it('reads what a filter selection holds from its facets', () => {
    const held = heldInFacets({ counterparty: { 'c-gialli': 9, 'c-rossi': 0 } });
    expect(bulkDiff({ counterparty: null, tags: {} }, held).remove).toEqual([
      { field: 'counterparty', value_key: 'c-gialli' },
    ]);
  });
});

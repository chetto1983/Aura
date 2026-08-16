import { describe, expect, it } from 'vitest';
import { DataStore } from '@svar-ui/filemanager-store';

/**
 * Pins the widget behaviour the file listing depends on, against the INSTALLED store rather
 * than against its documentation.
 *
 * A stored object's key is `chat/<assetID>.pdf` on purpose: the key travels into presigned
 * URLs and access logs, so the name a person gave the file is deliberately kept out of it.
 * The widget derives every row's label from the tail of `id`, which made the whole file
 * manager read as uuids.
 *
 * The escape is not documented, it is measured: `parseId` assigns `name` from the id
 * UNCONDITIONALLY -- so a name sent beside it is discarded -- except that it returns early
 * when `parent` is 0, and the tree base class then reads `parent || requestedFolder`, which
 * puts the row back in the folder being viewed. Skipping the derivation also skips `ext`,
 * which drives the icon, so the server sends that too.
 *
 * This test exists so an upgrade of @svar-ui/filemanager-store FAILS here instead of
 * silently restoring uuid labels in production, which no Go test could catch.
 */
function parseOne(row: Record<string, unknown>) {
  const store = new DataStore();
  store.initOnce();
  store.init({ data: [{ id: '/chat', type: 'folder', lazy: true }], mode: 'cards' });
  store.in.exec('provide-data', { id: '/chat', data: [row] });
  return store.getFile('/chat/b4e391e0-6141-4807-b8e5-88ca58f21162.pdf');
}

const key = '/chat/b4e391e0-6141-4807-b8e5-88ca58f21162.pdf';

describe('SVAR file store label derivation', () => {
  it('discards a name sent without the parent sentinel', () => {
    const parsed = parseOne({ id: key, type: 'file', name: 'colm2025_conference.pdf' });

    expect(parsed.name).toBe('b4e391e0-6141-4807-b8e5-88ca58f21162.pdf');
  });

  it('keeps the sent name, folder and icon when parent is 0', () => {
    const parsed = parseOne({
      id: key,
      type: 'file',
      name: 'colm2025_conference.pdf',
      ext: 'pdf',
      parent: 0,
    });

    expect(parsed.name).toBe('colm2025_conference.pdf');
    // The row must still land in the folder being viewed: parent 0 is a sentinel, never a
    // real parent, and a row that kept it would vanish from the listing.
    expect(parsed.parent).toBe('/chat');
    expect(parsed.ext).toBe('pdf');
    expect(parsed.type).toBe('file');
  });

  it('leaves the id alone, because it is the route back to the object', () => {
    const parsed = parseOne({
      id: key,
      type: 'file',
      name: 'colm2025_conference.pdf',
      ext: 'pdf',
      parent: 0,
    });

    expect(parsed.id).toBe(key);
  });
});

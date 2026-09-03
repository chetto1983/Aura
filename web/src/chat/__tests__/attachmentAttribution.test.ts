import { describe, expect, it } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { attachAssetsToUserMessages } from '../ExternalStoreChat_folds';
import type { Asset } from '../attachments/types';

function asset(id: string, over: Partial<Asset> = {}): Asset {
  return {
    id,
    status: 'processing',
    modality: 'image',
    file_name: `${id}.png`,
    mime_type: 'image/png',
    declared_size_bytes: 1,
    size_bytes: 1,
    ...over,
  };
}

function user(text: string, attachmentIds?: readonly string[]): ThreadMessageLike {
  return {
    role: 'user',
    content: [{ type: 'text', text }],
    ...(attachmentIds !== undefined ? { metadata: { custom: { attachmentIds } } } : {}),
  };
}

const assistant: ThreadMessageLike = { role: 'assistant', content: [{ type: 'text', text: 'ok' }] };

function attachmentsOf(message: ThreadMessageLike): readonly Asset[] {
  const custom = message.metadata?.custom as { attachments?: readonly Asset[] } | undefined;
  return custom?.attachments ?? [];
}

describe('attachAssetsToUserMessages', () => {
  // The live defect (2026-09-03, migration 0116): an image sent with the third message
  // rendered against the first, because the only rule available was "Nth asset onto Nth
  // user turn" and the image was the thread's first asset.
  it('puts an attachment on the turn that declares it, not on the first one', () => {
    const messages = [user('ciao'), assistant, user('chi sei?'), assistant, user('guarda', ['a1'])];

    const folded = attachAssetsToUserMessages(messages, [asset('a1')]);

    expect(attachmentsOf(folded[0]!)).toHaveLength(0);
    expect(attachmentsOf(folded[2]!)).toHaveLength(0);
    expect(attachmentsOf(folded[4]!).map((a) => a.id)).toEqual(['a1']);
  });

  it('keeps a declared turn ordered and whole when several files were sent at once', () => {
    const messages = [user('due file', ['a1', 'a2'])];

    const folded = attachAssetsToUserMessages(messages, [asset('a2'), asset('a1')]);

    expect(attachmentsOf(folded[0]!).map((a) => a.id)).toEqual(['a1', 'a2']);
  });

  // A turn saved before the column exists has nothing to declare, so the old rule is all
  // there is for it.
  it('still folds positionally for turns that declare nothing', () => {
    const messages = [user('vecchio'), assistant, user('anche vecchio')];

    const folded = attachAssetsToUserMessages(messages, [asset('a1'), asset('a2')]);

    expect(attachmentsOf(folded[0]!).map((a) => a.id)).toEqual(['a1']);
    expect(attachmentsOf(folded[2]!).map((a) => a.id)).toEqual(['a2']);
  });

  // A conversation continued across the deploy carries both kinds. The leftovers of the
  // old half must not pile onto a turn that already stated its own.
  it('never adds a leftover to a turn that declared its attachments', () => {
    const messages = [user('vecchio'), assistant, user('nuovo', ['a2'])];

    const folded = attachAssetsToUserMessages(messages, [asset('a1'), asset('a2')]);

    expect(attachmentsOf(folded[0]!).map((a) => a.id)).toEqual(['a1']);
    expect(attachmentsOf(folded[2]!).map((a) => a.id)).toEqual(['a2']);
  });

  // A declared id whose asset is gone renders nothing rather than a card for bytes that
  // no longer exist.
  it('drops a declared id with no live asset behind it', () => {
    const messages = [user('cancellato', ['gone']), assistant];

    const folded = attachAssetsToUserMessages(messages, [asset('gone', { status: 'deleted' })]);

    expect(attachmentsOf(folded[0]!)).toHaveLength(0);
  });

  // Documents are not a special case -- the ids are modality-blind on purpose, because a
  // PDF reaches the model through document scope and would otherwise have been lost.
  it('attributes a document exactly as it does an image', () => {
    const messages = [user('ciao'), assistant, user('leggi', ['doc'])];
    const pdf = asset('doc', { modality: 'document', mime_type: 'application/pdf' });

    const folded = attachAssetsToUserMessages(messages, [pdf]);

    expect(attachmentsOf(folded[0]!)).toHaveLength(0);
    expect(attachmentsOf(folded[2]!).map((a) => a.id)).toEqual(['doc']);
  });
});

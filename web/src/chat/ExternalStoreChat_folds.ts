import type { AppendMessage, ThreadMessageLike } from '@assistant-ui/react';
import type { Asset } from './attachments/types';

// Pure message/asset-fold helpers for the ExternalStoreChat lane (extracted so the component
// file stays under the 600-LOC cap — refactor-on-touch). NO React, NO runtime coupling: every
// function here maps a message list + assets to a new message list, unit-tested in isolation.

export function appendMessageText(message: AppendMessage): string {
  return message.content
    .filter((p): p is { type: 'text'; text: string } => p.type === 'text')
    .map((p) => p.text)
    .join('');
}

export function userMessage(text: string, attachments: readonly Asset[] = []): ThreadMessageLike {
  return {
    id: crypto.randomUUID(),
    role: 'user',
    content: [{ type: 'text', text }],
    ...(attachments.length > 0 ? { metadata: { custom: { attachments } } } : {}),
  };
}

function messageAttachments(message: ThreadMessageLike): readonly Asset[] {
  const metadata = message.metadata?.custom as { attachments?: readonly Asset[] } | undefined;
  return metadata?.attachments ?? [];
}

function withMessageAttachments(
  message: ThreadMessageLike,
  attachments: readonly Asset[],
): ThreadMessageLike {
  const custom = { ...(message.metadata?.custom ?? {}), attachments };
  return { ...message, metadata: { ...message.metadata, custom } };
}

// foldAssetsPositionally zips visible assets onto the messages of a given role using a
// positional heuristic: the Nth asset attaches to the Nth role-turn, extras pile on the
// last. `eligible` narrows which turns of that role may receive one.
//
// It is a guess, and it was wrong for user turns the moment a conversation had a turn
// before the attachment. Migration 0116 gave user turns their own record, so this is now
// their FALLBACK for rows saved before it. The D-15 agent-deliverable fold still relies on
// it outright: nothing records which assistant turn produced a file.
function foldAssetsPositionally(
  messages: readonly ThreadMessageLike[],
  assets: readonly Asset[],
  role: ThreadMessageLike['role'],
  eligible: (message: ThreadMessageLike) => boolean = () => true,
): ThreadMessageLike[] {
  const visibleAssets = assets.filter(
    (asset) => asset.status !== 'deleted' && asset.status !== 'canceled',
  );
  if (visibleAssets.length === 0) return [...messages];
  const targetIndexes = messages
    .map((message, index) => (message.role === role && eligible(message) ? index : -1))
    .filter((index) => index >= 0);
  if (targetIndexes.length === 0) return [...messages];
  const groups = new Map<number, Asset[]>();
  visibleAssets.forEach((asset, assetIndex) => {
    const target = targetIndexes[Math.min(assetIndex, targetIndexes.length - 1)];
    if (target === undefined) return;
    groups.set(target, [...(groups.get(target) ?? []), asset]);
  });
  return messages.map((message, index) => {
    const additions = groups.get(index);
    if (additions === undefined) return message;
    return withMessageAttachments(message, [...messageAttachments(message), ...additions]);
  });
}

/** The ids a user turn declares it was sent with (migration 0116), or [] for a turn that
 *  predates the column. */
function declaredAttachmentIDs(message: ThreadMessageLike): readonly string[] {
  const custom = message.metadata?.custom as { attachmentIds?: readonly string[] } | undefined;
  return custom?.attachmentIds ?? [];
}

/**
 * Attach each user turn's assets, by the ids the turn itself declares.
 *
 * The positional fold below is now the FALLBACK, used only for turns saved before the
 * server recorded what was sent with them. It had to be: an asset carries no message key,
 * so the Nth asset went to the Nth user turn, and an image sent with the third message
 * rendered against the first (measured 2026-09-03).
 *
 * A turn that declares ids gets exactly those, resolved against the thread's assets. An id
 * with no matching asset is dropped rather than rendered as a placeholder: the asset may
 * have been deleted, and a card for bytes that no longer exist is worse than no card.
 */
export function attachAssetsToUserMessages(
  messages: readonly ThreadMessageLike[],
  assets: readonly Asset[],
): ThreadMessageLike[] {
  const byID = new Map(assets.map((asset) => [asset.id, asset]));
  const claimed = new Set<string>();
  const resolved = messages.map((message) => {
    const declared = declaredAttachmentIDs(message);
    if (message.role !== 'user' || declared.length === 0) return message;
    const found = declared.flatMap((id) => {
      const asset = byID.get(id);
      if (asset === undefined || asset.status === 'deleted' || asset.status === 'canceled') {
        return [];
      }
      claimed.add(id);
      return [asset];
    });
    if (found.length === 0) return message;
    return withMessageAttachments(message, [...messageAttachments(message), ...found]);
  });
  // Only what no turn claimed still needs guessing — on a conversation saved since 0116
  // that is nothing at all.
  const unclaimed = assets.filter((asset) => !claimed.has(asset.id));
  if (unclaimed.length === 0) return resolved;
  // ...and only onto turns that declared nothing. A conversation continued across the
  // deploy has both kinds, and piling leftovers onto a turn that already stated its own
  // attachments would put a stray file under a message that never carried it.
  return foldAssetsPositionally(
    resolved,
    unclaimed,
    'user',
    (message) => declaredAttachmentIDs(message).length === 0,
  );
}

// D-15: fold `source_kind='agent'` deliverables onto the ASSISTANT turns that
// produced them (send_file's inline chip is synthesized client-side and never
// persisted, so the rehydrated tool card loses its asset_id — sseAdapter_snapshot).
// It MUST NOT fold agent assets onto user turns — that is the exact bug being fixed.
// Exported for the rehydration attribution test (pure fold, no runtime coupling).
export function foldAgentOntoAssistant(
  messages: readonly ThreadMessageLike[],
  agentAssets: readonly Asset[],
): ThreadMessageLike[] {
  return foldAssetsPositionally(messages, agentAssets, 'assistant');
}

export function replaceAssetInMessages(
  messages: readonly ThreadMessageLike[],
  asset: Asset,
): ThreadMessageLike[] {
  return messages.map((message) => {
    const attachments = messageAttachments(message);
    if (!attachments.some((item) => item.id === asset.id)) return message;
    return withMessageAttachments(
      message,
      attachments.map((item) => (item.id === asset.id ? asset : item)),
    );
  });
}

export function removeAssetFromMessages(
  messages: readonly ThreadMessageLike[],
  assetID: string,
): ThreadMessageLike[] {
  return messages.map((message) => {
    const attachments = messageAttachments(message);
    if (!attachments.some((item) => item.id === assetID)) return message;
    return withMessageAttachments(
      message,
      attachments.filter((item) => item.id !== assetID),
    );
  });
}

/**
 * True when an assistant message carries at least one non-empty TEXT part — a
 * real answer. Messages whose parts are exclusively machinery (tool-call /
 * reasoning / display) must NOT surface the Copy/Regenerate/TTS action bar
 * (operator directive: no message actions on tool cards or reasoning-only
 * turns). Works on both live-built and snapshot-rehydrated messages: the
 * runtime's converted message keeps the same content-part contract.
 */
export function hasAnswerText(message: ThreadMessageLike): boolean {
  if (typeof message.content === 'string') return message.content.trim().length > 0;
  return message.content.some(
    (part) => part.type === 'text' && typeof part.text === 'string' && part.text.trim().length > 0,
  );
}

export function assistantErrorMessage(text: string): ThreadMessageLike {
  return {
    id: crypto.randomUUID(),
    role: 'assistant',
    content: [{ type: 'text', text }],
    status: { type: 'incomplete', reason: 'error' },
  };
}

/** A caller-side abort, which is a cancellation rather than a failure: it must never be
 * folded into the transcript as an assistant error turn. */
export const isAbortError = (error: unknown) =>
  error instanceof DOMException && error.name === 'AbortError';

export function isAbortSignalAborted(signal: AbortSignal): boolean {
  return signal.aborted;
}

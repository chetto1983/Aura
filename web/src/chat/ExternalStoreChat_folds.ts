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

// foldAssetsPositionally zips visible assets onto the messages of a given role
// using a positional heuristic (an Asset carries no message/tool_call_id key): the
// Nth asset attaches to the Nth role-turn, extras pile on the last. Shared by the
// user-upload fold and the D-15 agent-deliverable fold (dupl-folded on touch).
function foldAssetsPositionally(
  messages: readonly ThreadMessageLike[],
  assets: readonly Asset[],
  role: ThreadMessageLike['role'],
): ThreadMessageLike[] {
  const visibleAssets = assets.filter(
    (asset) => asset.status !== 'deleted' && asset.status !== 'canceled',
  );
  if (visibleAssets.length === 0) return [...messages];
  const targetIndexes = messages
    .map((message, index) => (message.role === role ? index : -1))
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

export function attachAssetsToUserMessages(
  messages: readonly ThreadMessageLike[],
  assets: readonly Asset[],
): ThreadMessageLike[] {
  return foldAssetsPositionally(messages, assets, 'user');
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

export function assistantErrorMessage(text: string): ThreadMessageLike {
  return {
    id: crypto.randomUUID(),
    role: 'assistant',
    content: [{ type: 'text', text }],
    status: { type: 'incomplete', reason: 'error' },
  };
}

export function isAbortSignalAborted(signal: AbortSignal): boolean {
  return signal.aborted;
}

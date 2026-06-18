import type { ThreadMessageLike } from '@assistant-ui/react';
import { snapshotToThreadMessages } from '../sseAdapter';

// snapshotToMessages is the display-replay home for the MESSAGES_SNAPSHOT →
// ThreadMessageLike projection (D-06). Reopening a thread fetches
// GET /threads/{id}/messages (a MESSAGES_SNAPSHOT body) and rehydrates the chat
// lane: user prose, assistant prose, and tool turns — each tool turn carrying
// its re-derived `display` payload when the backend normalizer recognized it.
//
// It delegates to the kept-verbatim `snapshotToThreadMessages` reducer-mirror
// (sseAdapter.ts) so live folding and replay produce IDENTICAL part shapes —
// the single source of truth for the snapshot→message mapping. This module is
// the named, display-aware entry point the components import; it does NOT
// re-implement the projection (no drift, no duplication). The `.display`
// forward-compat lives in `toolCallsFromSnapshot` (sseAdapter.ts): a snapshot
// tool-call entry carrying a `display` field maps to a tool part with `.display`
// set, and is silently tolerated when absent.

/**
 * Project a persisted MESSAGES_SNAPSHOT body onto the visible assistant-ui chat
 * model (text + tool turns, in arrival order). A tool turn carrying a re-derived
 * `display` payload maps to a tool part with `.display` set so the DisplayRouter
 * renders the typed display on replay exactly as it does live.
 */
export function snapshotToMessages(snapshot: unknown): ThreadMessageLike[] {
  return snapshotToThreadMessages(snapshot);
}

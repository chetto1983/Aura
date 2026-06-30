import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getJSON, postJSON } from '../api/json';

// useConversations is the CHAT-02 / D-07/D-08 data layer over the plan-25-01 thin
// REST adapter (/api/conversations…). It mirrors useRuntimeHealth.ts exactly:
// React Query useQuery/useMutation, ALWAYS credentials: 'same-origin' (the SPA is
// served by the very binary exposing the routes, behind the Phase-24 RequireAuth
// whole-origin gate), retry: false so a transient failure surfaces immediately.
//
// Wire shapes are the Go store projections serialised with their Go field names
// (the structs carry no json tags): Conversation, SearchResult, RotEvent below.

/** GET /api/conversations row — conversations.Store.Conversation projection. */
export interface Conversation {
  readonly ID: string;
  /** Empty when the DB title is NULL; TitleSet distinguishes "" from unset. */
  readonly Title: string;
  readonly TitleSet: boolean;
  readonly IdentityID: string;
  readonly Status: string;
  readonly Model: string;
  readonly TotalInputTokens: number;
  readonly TotalOutputTokens: number;
  readonly TotalCachedTokens: number;
  readonly TotalCostUSD: number;
  /** RFC3339 created-at; drives recent-first ordering + the untitled fallback. */
  readonly CreatedAt: string;
}

/** GET /api/conversations/search hit — conversations.Store.SearchResult projection. */
export interface SearchResult {
  readonly ConversationID: string;
  readonly Seq: number;
  readonly Content: string;
  readonly Similarity: number;
}

/** GET /api/conversations/{id}/rot-events — microcompact ladder audit row (D-11). */
export interface RotEvent {
  readonly TS: string;
  readonly Action: string;
  readonly PairsDropped: number;
  readonly TokensBefore: number;
  readonly TokensAfter: number;
}

export const CONVERSATION_STATUS_ARCHIVED = 'archived';

/** Conversation status as the store reports it (active is the unarchived default). */
export function isArchived(conv: Conversation): boolean {
  return conv.Status === CONVERSATION_STATUS_ARCHIVED;
}

// The store renders a NULL title as "(untitled …)" via DisplayTitle on the CLI/
// channel side, but the REST projection returns the raw Title (empty when unset).
// The sidebar shows the operator-facing label, falling back to a localised
// "Untitled" caller-side so the row is never blank.
export function displayTitle(conv: Conversation, untitled: string): string {
  return conv.TitleSet && conv.Title.length > 0 ? conv.Title : untitled;
}

export function autoTitleFromPrompt(prompt: string): string {
  const compact = prompt
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/["'`]+/g, '');
  const stripped = compact.replace(/[.?!,:;]+$/u, '').trim();
  if (stripped.length <= 80) return stripped;
  const head = stripped.slice(0, 80).trimEnd();
  const lastSpace = head.lastIndexOf(' ');
  return `${(lastSpace >= 32 ? head.slice(0, lastSpace) : head).trimEnd()}...`;
}

export function createConversation(initialPrompt?: string): Promise<Conversation> {
  const title =
    initialPrompt === undefined || initialPrompt.trim().length === 0
      ? ''
      : autoTitleFromPrompt(initialPrompt);
  // An empty title posts `{}` (the store leaves the row untitled) — a body the canonical
  // postJSON always serialises, while the backend decoder treats it the same as no body.
  return postJSON<Conversation>('/api/conversations', title.length > 0 ? { title } : {});
}

// A 204 No Content POST/DELETE: no body to parse, a non-2xx is a thrown error the
// mutation surfaces (React Query sets isError; the UI shows the failure copy).
async function mutate(url: string, method: 'POST' | 'DELETE', body?: unknown): Promise<void> {
  const init: RequestInit = {
    method,
    credentials: 'same-origin',
    ...(body !== undefined
      ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
      : {}),
  };
  const res = await fetch(url, init);
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
}

export const CONVERSATIONS_KEY = 'conversations';
export const CONVERSATION_KEY = 'conversation';
export const CONVERSATION_SEARCH_KEY = 'conversation-search';
export const CONVERSATION_ROT_EVENTS_KEY = 'conversation-rot-events';

/**
 * GET /api/conversations/{id} — the single row incl. session-cumulative token/cost
 * aggregates (D-10 footer reload seed). Disabled (no fetch) for an empty id.
 */
export function useConversation(conversationId: string) {
  return useQuery({
    queryKey: [CONVERSATION_KEY, conversationId],
    queryFn: () =>
      getJSON<Conversation>(`/api/conversations/${encodeURIComponent(conversationId)}`),
    enabled: conversationId.length > 0,
    retry: false,
  });
}

/** GET /api/conversations(?archived=true) — recent-first list (the store orders it). */
export function useConversations(includeArchived: boolean) {
  return useQuery({
    queryKey: [CONVERSATIONS_KEY, includeArchived],
    queryFn: () =>
      getJSON<Conversation[]>(
        includeArchived ? '/api/conversations?archived=true' : '/api/conversations',
      ),
    retry: false,
  });
}

/**
 * GET /api/conversations/search?q= — FTS snippet hits (D-08). Disabled (no fetch)
 * for an empty/whitespace query so the panel shows its idle state, not a 400.
 */
export function useConversationSearch(query: string) {
  const trimmed = query.trim();
  return useQuery({
    queryKey: [CONVERSATION_SEARCH_KEY, trimmed],
    queryFn: () =>
      getJSON<SearchResult[]>(`/api/conversations/search?q=${encodeURIComponent(trimmed)}`),
    enabled: trimmed.length > 0,
    retry: false,
  });
}

/** GET /api/conversations/{id}/rot-events — microcompact markers for the gauge (D-11). */
export function useCreateConversation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createConversation,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] });
    },
  });
}

export function useConversationRotEvents(conversationId: string) {
  return useQuery({
    queryKey: [CONVERSATION_ROT_EVENTS_KEY, conversationId],
    queryFn: () =>
      getJSON<RotEvent[]>(`/api/conversations/${encodeURIComponent(conversationId)}/rot-events`),
    enabled: conversationId.length > 0,
    retry: false,
  });
}

export interface RenameVars {
  readonly id: string;
  readonly title: string;
}

/** POST /api/conversations/{id}/rename — inline title edit; invalidates the list. */
export function useRenameConversation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, title }: RenameVars) =>
      mutate(`/api/conversations/${encodeURIComponent(id)}/rename`, 'POST', { title }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] });
    },
  });
}

/** POST /api/conversations/{id}/archive — D-07 reversible primary action. */
export function useArchiveConversation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      mutate(`/api/conversations/${encodeURIComponent(id)}/archive`, 'POST'),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] });
    },
  });
}

/** POST /api/conversations/{id}/unarchive — restores an archived row to active. */
export function useUnarchiveConversation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      mutate(`/api/conversations/${encodeURIComponent(id)}/unarchive`, 'POST'),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] });
    },
  });
}

/** DELETE /api/conversations/{id} — D-07 hard delete (gated behind the confirm UI). */
export function useDeleteConversation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => mutate(`/api/conversations/${encodeURIComponent(id)}`, 'DELETE'),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] });
    },
  });
}

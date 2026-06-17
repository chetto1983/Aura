import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { highlightSegments } from './searchHighlight';
import {
  displayTitle,
  useConversations,
  useConversationSearch,
  type Conversation,
} from './useConversations';

// SearchPanel is the CHAT-02 / D-08 FTS surface over GET /api/conversations/search.
// Each hit projects ConversationID + Seq + Content (the snippet); the store's
// SearchResult carries no title, so we enrich the title from the already-cached
// conversation list. Clicking a hit opens that thread at the match: it navigates
// to /c/:conversationId (deep-link URL the operator can share) AND calls onOpen so
// the AppShell binds the active thread + target seq for scroll-to-match.
//
// Snippets render as React text nodes (auto-escaped); the highlighted match is
// composed from safe <mark> element wrapping, never raw HTML (T-25-15).

export interface SearchPanelProps {
  /** Open the matched thread (AppShell binds activeThreadId + scrolls to seq). */
  readonly onOpen: (conversationId: string, seq: number) => void;
}

export function SearchPanel({ onOpen }: SearchPanelProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const trimmed = query.trim();

  const { data: hits, isFetching } = useConversationSearch(query);
  // The hits carry no title; enrich from the cached conversation list (archived
  // included so a hit in an archived thread still shows its title).
  const { data: conversations } = useConversations(true);
  const titleById = useMemo(() => {
    const map = new Map<string, Conversation>();
    for (const conv of conversations ?? []) map.set(conv.ID, conv);
    return map;
  }, [conversations]);

  function open(conversationId: string, seq: number) {
    onOpen(conversationId, seq);
    void navigate(`/c/${encodeURIComponent(conversationId)}`);
  }

  const results = hits ?? [];

  return (
    <div className="flex flex-col gap-2">
      <label htmlFor="conversation-search" className="sr-only">
        {t('conversations.search.label')}
      </label>
      <input
        id="conversation-search"
        type="search"
        value={query}
        onChange={(event) => {
          setQuery(event.target.value);
        }}
        placeholder={t('conversations.search.placeholder')}
        className="min-h-[var(--row-h)] w-full rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      />

      {trimmed.length === 0 ? null : isFetching && results.length === 0 ? (
        <p className="text-sm text-text-muted">{t('conversations.search.searching')}</p>
      ) : results.length === 0 ? (
        <div className="py-2">
          <p className="text-sm font-medium text-text">{t('conversations.search.empty.heading')}</p>
          <p className="mt-1 text-[0.8125rem] text-text-muted">
            {t('conversations.search.empty.body', { query: trimmed })}
          </p>
        </div>
      ) : (
        <ul className="flex flex-col gap-1">
          {results.map((hit) => {
            const conv = titleById.get(hit.ConversationID);
            const title = conv
              ? displayTitle(conv, t('conversations.untitled'))
              : t('conversations.untitled');
            return (
              <li key={`${hit.ConversationID}-${String(hit.Seq)}`}>
                <button
                  type="button"
                  onClick={() => {
                    open(hit.ConversationID, hit.Seq);
                  }}
                  className="flex w-full flex-col gap-0.5 rounded-[var(--radius-md)] px-2 py-1.5 text-left outline-none hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                >
                  <span className="truncate text-[0.8125rem] font-medium text-accent">{title}</span>
                  <span className="line-clamp-2 text-[0.8125rem] text-text-muted">
                    {highlightSegments(hit.Content, trimmed).map((seg, i) =>
                      seg.match ? (
                        <mark
                          key={i}
                          className="bg-transparent font-medium text-text underline decoration-accent"
                        >
                          {seg.text}
                        </mark>
                      ) : (
                        <span key={i}>{seg.text}</span>
                      ),
                    )}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

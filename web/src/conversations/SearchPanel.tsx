import { useMemo, useState } from 'react';
import { Search, SearchX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { highlightSegments } from './searchHighlight';
import {
  displayTitle,
  useConversations,
  useConversationSearch,
  type Conversation,
} from './useConversations';
import { Button } from '@/components/ui/button';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';

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
      <div className="relative">
        <Search
          data-icon
          aria-hidden="true"
          className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-faint"
        />
        <Input
          id="conversation-search"
          type="search"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
          }}
          placeholder={t('conversations.search.placeholder')}
          className="bg-surface-2 pl-9 text-sm"
        />
      </div>

      {trimmed.length === 0 ? null : isFetching && results.length === 0 ? (
        <div aria-label={t('conversations.search.searching')} className="flex flex-col gap-2">
          <Skeleton className="h-14 rounded-md" />
          <Skeleton className="h-14 rounded-md" />
        </div>
      ) : results.length === 0 ? (
        <Empty className="flex-none border border-dashed border-border bg-surface-2/40 py-6">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchX data-icon aria-hidden="true" className="size-5" />
            </EmptyMedia>
            <EmptyTitle className="text-sm">{t('conversations.search.empty.heading')}</EmptyTitle>
            <EmptyDescription className="text-[0.8125rem]">
              {t('conversations.search.empty.body', { query: trimmed })}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="flex flex-col gap-1">
          {results.map((hit) => {
            const conv = titleById.get(hit.ConversationID);
            const title = conv
              ? displayTitle(conv, t('conversations.untitled'))
              : t('conversations.untitled');
            return (
              <li key={`${hit.ConversationID}-${String(hit.Seq)}`}>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    open(hit.ConversationID, hit.Seq);
                  }}
                  className="h-auto min-h-14 w-full flex-col items-start gap-0.5 whitespace-normal rounded-md px-2 py-2 text-left font-normal hover:bg-surface-2"
                >
                  <span className="truncate text-[0.8125rem] font-medium text-accent-text">
                    {title}
                  </span>
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
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

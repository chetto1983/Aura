import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router-dom';
import { ExternalStoreChat } from './chat/ExternalStoreChat';
import { ConversationSidebar } from './conversations/ConversationSidebar';
import { SearchPanel } from './conversations/SearchPanel';
import { RuntimeHealthPanel } from './health/RuntimeHealthPanel';
import { LanguageSwitcher } from './i18n/LanguageSwitcher';

const MODES = ['chat', 'tree', 'graph', 'displays', 'settings'] as const;

export function AppShell() {
  const { t } = useTranslation();
  // The active conversation id drives which thread the chat lane POSTs against
  // (CHAT-02 — 25-04 resolves the 25-03 stub). The sidebar selects it; a /c/:id
  // deep link (search → open at match) seeds it from the route param.
  const { id: routeId } = useParams<{ id: string }>();
  const [selectedId, setSelectedId] = useState('');
  // Track the last route param so a navigation (deep link) wins over the prior
  // in-component selection WITHOUT a state-syncing effect (React "adjust state
  // during render" — the only re-render is this same pass, no cascade).
  const [lastRouteId, setLastRouteId] = useState(routeId ?? '');
  if ((routeId ?? '') !== lastRouteId) {
    setLastRouteId(routeId ?? '');
    setSelectedId(routeId ?? '');
  }
  const activeThreadId = selectedId;

  function selectThread(id: string) {
    setSelectedId(id);
  }

  return (
    <div className="grid h-dvh overflow-hidden bg-bg text-text [grid-template-rows:auto_1fr_auto]">
      <header className="flex min-h-14 flex-wrap items-center gap-x-3 gap-y-2 border-b border-border px-3 py-2 sm:px-4">
        <img src="/logo.png" alt="Aura" width={24} height={24} className="rounded-sm" />
        <span className="font-sans text-sm font-medium tracking-wide text-text">Aura</span>
        <nav
          aria-label={t('shell.primaryNav')}
          className="order-last flex w-full flex-wrap gap-1 text-text-muted sm:order-none sm:ml-6 sm:w-auto"
        >
          {MODES.map((mode) => (
            <span
              key={mode}
              className="rounded-md px-2 py-1 text-xs aria-[current]:bg-surface-2 aria-[current]:text-text"
              aria-current={mode === 'chat' ? 'page' : undefined}
            >
              {t(`shell.modes.${mode}`)}
            </span>
          ))}
        </nav>
        <LanguageSwitcher className="ml-auto" />
      </header>

      <main className="grid min-h-0 grid-cols-1 grid-rows-[auto_minmax(0,1fr)_auto] lg:grid-cols-[14rem_minmax(0,1fr)_18rem] lg:grid-rows-1">
        {/* Conversation manager (CHAT-02) — search panel + recent-first sidebar
            list, replacing the placeholder section labels. Selection drives the
            chat lane's threadId. */}
        <aside
          aria-label={t('shell.navigation')}
          className="flex min-h-0 flex-col gap-2 border-b border-border bg-surface px-3 py-3 lg:border-b-0 lg:border-r"
        >
          <SearchPanel onOpen={(id) => { selectThread(id); }} />
          <div className="min-h-0 flex-1 overflow-hidden">
            <ConversationSidebar activeId={activeThreadId} onSelect={selectThread} />
          </div>
        </aside>

        {/* The Core-Value chat lane (CHAT-01). The sidebar binds activeThreadId
            (25-04); the runtime footer (Task 2) consumes onUsage; the branch
            picker (25-07) mounts onto the same lane. */}
        <section aria-label={t('shell.chatRegion')} className="min-h-0 bg-bg">
          <ExternalStoreChat threadId={activeThreadId} />
        </section>

        <aside
          aria-label={t('shell.displayWorkspace')}
          className="min-h-0 overflow-y-auto border-t border-border bg-surface lg:border-l lg:border-t-0"
        >
          <RuntimeHealthPanel />
        </aside>
      </main>
    </div>
  );
}

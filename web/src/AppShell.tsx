import { useTranslation } from 'react-i18next';
import { ExternalStoreChat } from './chat/ExternalStoreChat';
import { RuntimeHealthPanel } from './health/RuntimeHealthPanel';
import { LanguageSwitcher } from './i18n/LanguageSwitcher';

const MODES = ['chat', 'tree', 'graph', 'displays', 'settings'] as const;

export function AppShell() {
  const { t } = useTranslation();
  // The active conversation id. The conversation sidebar (plan 25-02 frontend)
  // will own selection/creation and set this; the chat lane reads it to POST
  // /agent/run against the right thread. Empty until the sidebar lands.
  const activeThreadId = '';

  return (
    <div className="grid h-dvh overflow-hidden bg-bg text-text [grid-template-rows:auto_1fr]">
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
        <aside
          aria-label={t('shell.navigation')}
          className="grid gap-2 border-b border-border bg-surface px-3 py-3 sm:grid-cols-3 lg:flex lg:flex-col lg:gap-3 lg:border-b-0 lg:border-r"
        >
          <p className="text-[0.6875rem] font-medium uppercase tracking-wider text-text-faint">
            {t('shell.sections.investigations')}
          </p>
          <p className="text-[0.6875rem] font-medium uppercase tracking-wider text-text-faint">
            {t('shell.sections.searxngSocket')}
          </p>
          <p className="text-[0.6875rem] font-medium uppercase tracking-wider text-text-faint">
            {t('shell.sections.neo4jMcp')}
          </p>
        </aside>

        {/* The Core-Value chat lane (CHAT-01). The conversation/threadId binding
            arrives with the conversation sidebar (plan 25-02 frontend) — until
            then the lane mounts against the current thread seam; the runtime
            footer (25-04) and branch picker (25-07) mount onto the same lane. */}
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

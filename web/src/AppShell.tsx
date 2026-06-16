import { useTranslation } from 'react-i18next';
import { RuntimeHealthPanel } from './health/RuntimeHealthPanel';
import { LanguageSwitcher } from './i18n/LanguageSwitcher';

const MODES = ['chat', 'tree', 'graph', 'displays', 'settings'] as const;

export function AppShell() {
  const { t } = useTranslation();

  return (
    <div className="grid h-dvh grid-rows-[auto_1fr] bg-bg text-text">
      <header className="flex items-center gap-3 border-b border-border px-4 py-2">
        <img src="/logo.png" alt="Aura" width={24} height={24} className="rounded-sm" />
        <span className="font-sans text-sm font-medium tracking-wide text-text">Aura</span>
        <nav aria-label={t('shell.primaryNav')} className="ml-6 flex gap-1 text-text-muted">
          {MODES.map((mode) => (
            <span
              key={mode}
              className="rounded-md px-2 py-1 text-xs aria-[current]:text-text aria-[current]:bg-surface-2"
              aria-current={mode === 'chat' ? 'page' : undefined}
            >
              {t(`shell.modes.${mode}`)}
            </span>
          ))}
        </nav>
        <LanguageSwitcher className="ml-auto" />
      </header>

      <main className="grid min-h-0 grid-cols-[14rem_1fr_18rem]">
        <aside
          aria-label={t('shell.navigation')}
          className="flex flex-col gap-3 border-r border-border bg-surface px-3 py-3"
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

        <section aria-label={t('shell.chatRegion')} className="min-h-0 bg-bg" />

        <aside
          aria-label={t('shell.displayWorkspace')}
          className="min-h-0 overflow-y-auto border-l border-border bg-surface"
        >
          <RuntimeHealthPanel />
        </aside>
      </main>
    </div>
  );
}

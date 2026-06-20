import { lazy, Suspense, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

// GovernanceWorkspace is the lazy default export the AppShell mounts when surface==='governance'
// (its own Vite chunk — D-01, mirroring the Phase-27 graph swap). It is the read-only governance
// surface: a role="tablist" tab strip (MCP / Skills / Scheduler) over a persistent info-styled
// read-only banner, rendering the active board. Each board is a master-list + detail panel
// (BoardLayout: lg two-column grid, mobile bottom sheet) over the Plan-02 reads, with the full
// loading / empty / error / error-auth state contract (governanceView).
//
// The three boards are lazy so a board's data hooks load only when its tab is first opened;
// the workspace itself owns only the active-tab state + the accessible tablist wiring (arrow
// keys are native to the button tablist via roving focus + aria-selected). The onboarding
// wizard is a SEPARATE full-screen overlay (Plan 06), never a tab here.

const McpBoard = lazy(() => import('./McpBoard').then((m) => ({ default: m.McpBoard })));
const SkillsBoard = lazy(() => import('./SkillsBoard').then((m) => ({ default: m.SkillsBoard })));
const SchedulerBoard = lazy(() =>
  import('./SchedulerBoard').then((m) => ({ default: m.SchedulerBoard })),
);

type GovTab = 'mcp' | 'skills' | 'scheduler';

const TABS: readonly GovTab[] = ['mcp', 'skills', 'scheduler'];

export default function GovernanceWorkspace() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<GovTab>('mcp');
  const tabRefs = useRef<Record<GovTab, HTMLButtonElement | null>>({
    mcp: null,
    skills: null,
    scheduler: null,
  });
  const tablistId = useId();

  function focusTab(next: GovTab) {
    setTab(next);
    tabRefs.current[next]?.focus();
  }

  function onTabKeyDown(event: React.KeyboardEvent, index: number) {
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
      event.preventDefault();
      focusTab(TABS[(index + 1) % TABS.length] ?? 'mcp');
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
      event.preventDefault();
      focusTab(TABS[(index - 1 + TABS.length) % TABS.length] ?? 'mcp');
    } else if (event.key === 'Home') {
      event.preventDefault();
      focusTab('mcp');
    } else if (event.key === 'End') {
      event.preventDefault();
      focusTab('scheduler');
    }
  }

  return (
    <section aria-label={t('governance.title')} className="flex h-full min-h-0 flex-col bg-bg">
      {/* Read-only banner — info-styled, persistent (sets the read-only expectation, D-02). */}
      <p
        role="note"
        className="shrink-0 border-b border-border bg-surface px-4 py-2 text-[13px] text-info"
      >
        {t('governance.readOnlyBanner')}
      </p>

      {/* Tab strip — a proper role=tablist with the reserved accent on the active tab. */}
      <div
        role="tablist"
        aria-label={t('governance.title')}
        className="flex shrink-0 items-center gap-1 border-b border-border bg-surface px-2 py-1"
      >
        {TABS.map((name, index) => {
          const selected = tab === name;
          return (
            <button
              key={name}
              type="button"
              role="tab"
              id={`${tablistId}-tab-${name}`}
              aria-selected={selected}
              aria-controls={`${tablistId}-panel`}
              tabIndex={selected ? 0 : -1}
              ref={(el) => {
                tabRefs.current[name] = el;
              }}
              onKeyDown={(e) => {
                onTabKeyDown(e, index);
              }}
              onClick={() => {
                setTab(name);
              }}
              className={`min-h-[44px] rounded-md px-4 py-2 text-[13px] font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                selected
                  ? 'bg-accent text-on-accent'
                  : 'text-text-muted hover:bg-surface-2 hover:text-text'
              }`}
            >
              {t(`governance.tabs.${name}`)}
            </button>
          );
        })}
      </div>

      <div
        id={`${tablistId}-panel`}
        role="tabpanel"
        aria-labelledby={`${tablistId}-tab-${tab}`}
        className="min-h-0 flex-1"
      >
        <Suspense
          fallback={
            <div
              role="status"
              className="grid h-full place-items-center p-8 text-sm text-text-muted"
            >
              {t('governance.loading')}
            </div>
          }
        >
          {tab === 'mcp' ? <McpBoard /> : tab === 'skills' ? <SkillsBoard /> : <SchedulerBoard />}
        </Suspense>
      </div>
    </section>
  );
}

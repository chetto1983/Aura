import { lazy, Suspense, useRef, useState } from 'react';
import { CalendarClock, ScrollText, Server, Sparkles } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

const McpBoard = lazy(() => import('./McpBoard').then((m) => ({ default: m.McpBoard })));
const SkillsBoard = lazy(() => import('./SkillsBoard').then((m) => ({ default: m.SkillsBoard })));
const SchedulerBoard = lazy(() =>
  import('./SchedulerBoard').then((m) => ({ default: m.SchedulerBoard })),
);
// The per-identity activity feed lives here, not in Settings: it is a log to read, not a
// control to set, and it shares the roster + governance.write gate with the other boards.
const AdminAuditView = lazy(() =>
  import('../audit/AdminAuditView').then((m) => ({ default: m.AdminAuditView })),
);

type GovTab = 'mcp' | 'skills' | 'scheduler' | 'audit';

const TABS: readonly GovTab[] = ['mcp', 'skills', 'scheduler', 'audit'];

const TAB_ICON: Record<GovTab, typeof Server> = {
  mcp: Server,
  skills: Sparkles,
  scheduler: CalendarClock,
  audit: ScrollText,
};

export default function GovernanceWorkspace() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<GovTab>('mcp');
  const tabRefs = useRef<Record<GovTab, HTMLButtonElement | null>>({
    mcp: null,
    skills: null,
    scheduler: null,
    audit: null,
  });

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
      focusTab('audit');
    }
  }

  return (
    <section aria-label={t('governance.title')} className="flex h-full min-h-0 flex-col bg-bg">
      <Tabs
        value={tab}
        onValueChange={(value) => {
          setTab(value as GovTab);
        }}
        className="h-full min-h-0 gap-0"
      >
        <div className="shrink-0 border-b border-border bg-surface px-2 py-1">
          <TabsList
            aria-label={t('governance.title')}
            className="grid w-full grid-cols-4 justify-stretch"
          >
            {TABS.map((name, index) => {
              const Icon = TAB_ICON[name];
              return (
                <TabsTrigger
                  key={name}
                  value={name}
                  tabIndex={tab === name ? 0 : -1}
                  ref={(el) => {
                    tabRefs.current[name] = el;
                  }}
                  onClick={() => {
                    setTab(name);
                  }}
                  onKeyDown={(event) => {
                    onTabKeyDown(event, index);
                  }}
                  className="min-w-0 px-2 sm:px-4"
                >
                  <Icon data-icon="inline-start" aria-hidden="true" focusable="false" />
                  <span data-tab-label className="min-w-0 truncate">
                    {t(`governance.tabs.${name}`)}
                  </span>
                </TabsTrigger>
              );
            })}
          </TabsList>
        </div>

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
          <TabsContent value={tab} className="min-h-0">
            {tab === 'mcp' ? (
              <McpBoard />
            ) : tab === 'skills' ? (
              <SkillsBoard />
            ) : tab === 'scheduler' ? (
              <SchedulerBoard />
            ) : (
              // The boards own their own scroll; the audit feed is a plain section, so the
              // tab pane supplies the scroller and page gutters it would otherwise lack.
              <div className="h-full min-h-0 overflow-y-auto">
                <div className="mx-auto w-full max-w-5xl px-4 py-6 sm:px-6">
                  <AdminAuditView />
                </div>
              </div>
            )}
          </TabsContent>
        </Suspense>
      </Tabs>
    </section>
  );
}

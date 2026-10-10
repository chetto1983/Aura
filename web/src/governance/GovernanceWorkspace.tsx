import { lazy, Suspense, useState } from 'react';
import { CalendarClock, ScrollText, Server, Sparkles } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { SectionRail } from '@/components/SectionRail';

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

type GovSection = 'mcp' | 'skills' | 'scheduler' | 'audit';

const SECTION_ICONS: Record<GovSection, LucideIcon> = {
  mcp: Server,
  skills: Sparkles,
  scheduler: CalendarClock,
  audit: ScrollText,
};

const SECTIONS: readonly GovSection[] = ['mcp', 'skills', 'scheduler', 'audit'];

// Governance is a rail + pane, the same shape as Settings and on the same SectionRail: a
// sidebar column from `lg` up, a scrollable strip above the board below it. It used to be a
// four-column shadcn tablist that gave every board an equal 25% of the width and truncated
// its label to fit — "Pianificazione" lost half its letters on a phone.
export default function GovernanceWorkspace() {
  const { t } = useTranslation();
  const [section, setSection] = useState<GovSection>('mcp');

  return (
    <section
      aria-label={t('governance.title')}
      className="flex h-full min-h-0 flex-col bg-bg lg:flex-row"
    >
      <SectionRail
        id="governance"
        label={t('governance.rail.label')}
        groups={[
          {
            id: 'boards',
            items: SECTIONS.map((id) => ({
              id,
              icon: SECTION_ICONS[id],
              label: t(`governance.sections.${id}`),
            })),
          },
        ]}
        activeId={section}
        onSelect={(id) => {
          setSection(id as GovSection);
        }}
      />

      <div className="min-h-0 min-w-0 flex-1">
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
          <GovernancePane section={section} />
        </Suspense>
      </div>
    </section>
  );
}

function GovernancePane({ section }: { readonly section: GovSection }) {
  switch (section) {
    case 'mcp':
      return <McpBoard />;
    case 'skills':
      return <SkillsBoard />;
    case 'scheduler':
      return <SchedulerBoard />;
    case 'audit':
      // The boards own their own scroll; the audit feed is a plain section, so its pane
      // supplies the scroller and page gutters it would otherwise lack.
      return (
        <div className="h-full min-h-0 overflow-y-auto">
          <div className="mx-auto w-full max-w-5xl px-4 py-6 sm:px-6">
            <AdminAuditView />
          </div>
        </div>
      );
  }
}

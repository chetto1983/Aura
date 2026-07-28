import { Download, Plus, ScrollText, Search } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

// SkillsToolbar — the single header of the skills board: a name/description search, the
// three state filters, and the two board actions.
//
// It replaces the four lifecycle TABS. With a few dozen skills a filter beats a tab: a tab
// hides the other states behind a click and makes you remember where a skill lives, a chip
// narrows what is already in front of you and keeps the count visible. 'Pending' is gone
// entirely (amendment #97 — a written skill is live), and 'Archived' was never a place to
// navigate to: it is a state of a handful of rows.

/** The state filter applied to the merged skill list. */
export type SkillsFilter = 'all' | 'active' | 'archived';

const SKILLS_FILTERS: readonly SkillsFilter[] = ['all', 'active', 'archived'];

export interface SkillsToolbarProps {
  readonly query: string;
  readonly onQueryChange: (next: string) => void;
  readonly filter: SkillsFilter;
  readonly onFilterChange: (next: SkillsFilter) => void;
  /** Row counts per filter, rendered next to each chip label. */
  readonly counts: Readonly<Record<SkillsFilter, number>>;
  /** True while the audit ledger is showing (the Audit toggle is pressed). */
  readonly auditOpen: boolean;
  readonly onToggleAudit: (origin: HTMLElement | null) => void;
  readonly onInstall: (origin: HTMLElement | null) => void;
  /** Open the editor on a blank skill. */
  readonly onNew: () => void;
}

export function SkillsToolbar({
  query,
  onQueryChange,
  filter,
  onFilterChange,
  counts,
  auditOpen,
  onToggleAudit,
  onInstall,
  onNew,
}: SkillsToolbarProps) {
  const { t } = useTranslation();
  const installLabel = t('governance.skills.installSkill');
  const newLabel = t('governance.skills.newSkill');
  const auditLabel = t('governance.skills.stages.audit');

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-surface px-2 py-2">
      <label className="relative min-w-[10rem] flex-1">
        <span className="sr-only">{t('governance.skills.searchLabel')}</span>
        <Search
          aria-hidden="true"
          focusable="false"
          className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-muted"
        />
        <Input
          type="search"
          value={query}
          placeholder={t('governance.skills.searchPlaceholder')}
          onChange={(e) => {
            onQueryChange(e.target.value);
          }}
          className="pl-9"
        />
      </label>

      <div role="group" aria-label={t('governance.skills.filterLabel')} className="flex gap-1">
        {SKILLS_FILTERS.map((name) => {
          const on = filter === name;
          return (
            <Button
              key={name}
              type="button"
              variant={on ? 'default' : 'outline'}
              aria-pressed={on}
              onClick={() => {
                onFilterChange(name);
              }}
              className="px-3"
            >
              {t(`governance.skills.filter.${name}`)}
              <span data-count className="ml-1.5 font-mono text-[11.5px] tabular-nums opacity-75">
                {counts[name]}
              </span>
            </Button>
          );
        })}
      </div>

      <div className="ml-auto flex gap-2">
        <Button
          type="button"
          variant="outline"
          aria-pressed={auditOpen}
          onClick={(e) => {
            onToggleAudit(e.currentTarget);
          }}
          className="px-3"
        >
          <ScrollText data-icon="inline-start" aria-hidden="true" focusable="false" />
          <span data-action-label className="hidden sm:inline">
            {auditLabel}
          </span>
        </Button>
        <Button
          type="button"
          variant="outline"
          aria-label={installLabel}
          title={installLabel}
          className="px-3 sm:px-4"
          onClick={(e) => {
            onInstall(e.currentTarget);
          }}
        >
          <Download data-icon="inline-start" aria-hidden="true" focusable="false" />
          <span data-action-label className="hidden sm:inline">
            {installLabel}
          </span>
        </Button>
        <Button
          type="button"
          aria-label={newLabel}
          title={newLabel}
          className="px-3 sm:px-4"
          onClick={onNew}
        >
          <Plus data-icon="inline-start" aria-hidden="true" focusable="false" />
          <span data-action-label className="hidden sm:inline">
            {newLabel}
          </span>
        </Button>
      </div>
    </div>
  );
}

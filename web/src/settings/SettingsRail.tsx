import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  SETTINGS_GROUPS,
  type SettingsGroupId,
  type SettingsSectionDef,
  type SettingsSectionId,
} from './settingsSections';
import { cn } from '@/lib/utils';

interface SettingsRailProps {
  readonly sections: readonly SettingsSectionDef[];
  readonly activeId: SettingsSectionId;
  readonly onSelect: (id: SettingsSectionId) => void;
}

// One nav, two layouts, no duplicated DOM: a grouped column from `lg` up (NN/G — a vertical
// list on the left is scanned in fewer fixations than a horizontal one, and it has room for
// real labels), and a single horizontally scrollable strip below it, where a column would eat
// the pane. The group captions stay `sr-only` on the strip rather than `hidden`, so they keep
// labelling their lists for a screen reader at every width.
export function SettingsRail({ sections, activeId, onSelect }: SettingsRailProps) {
  const { t } = useTranslation();
  const activeRef = useRef<HTMLButtonElement | null>(null);
  // On the narrow strip the remembered pane can sit past the right edge, so the rail would
  // open showing a selection the operator cannot see. Scrolling it into view is a no-op in
  // the desktop column, where every item is already on screen.
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [activeId]);
  const groups = SETTINGS_GROUPS.map((group) => ({
    group,
    items: sections.filter((section) => section.group === group),
  })).filter((entry) => entry.items.length > 0);

  return (
    <nav
      aria-label={t('settings.rail.label')}
      className={cn(
        'flex shrink-0 gap-1 overflow-x-auto border-b border-border bg-surface px-2 py-2',
        'lg:w-64 lg:flex-col lg:gap-5 lg:overflow-x-visible lg:overflow-y-auto lg:border-r lg:border-b-0 lg:px-3 lg:py-5',
      )}
    >
      {groups.map(({ group, items }) => (
        <SettingsRailGroup
          key={group}
          group={group}
          items={items}
          activeId={activeId}
          activeRef={activeRef}
          onSelect={onSelect}
        />
      ))}
    </nav>
  );
}

function SettingsRailGroup({
  group,
  items,
  activeId,
  activeRef,
  onSelect,
}: {
  readonly group: SettingsGroupId;
  readonly items: readonly SettingsSectionDef[];
  readonly activeId: SettingsSectionId;
  readonly activeRef: React.RefObject<HTMLButtonElement | null>;
  readonly onSelect: (id: SettingsSectionId) => void;
}) {
  const { t } = useTranslation();
  const captionId = `settings-rail-${group}`;

  return (
    <div className="flex gap-1 lg:flex-col lg:gap-1.5">
      <p
        id={captionId}
        className="max-lg:sr-only px-2 text-xs font-semibold tracking-wide text-accent-text uppercase"
      >
        {t(`settings.groups.${group}`)}
      </p>
      <ul aria-labelledby={captionId} className="flex gap-1 lg:flex-col lg:gap-0.5">
        {items.map((section) => {
          const Icon = section.icon;
          const isActive = section.id === activeId;
          return (
            <li key={section.id}>
              <button
                type="button"
                ref={isActive ? activeRef : undefined}
                aria-current={isActive ? 'page' : undefined}
                onClick={() => {
                  onSelect(section.id);
                }}
                className={cn(
                  'flex min-h-11 w-full items-center gap-2 rounded-md px-3 text-left text-[13.5px] font-medium whitespace-nowrap transition-colors',
                  'focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
                  isActive
                    ? 'bg-primary text-primary-foreground'
                    : 'text-text-muted hover:bg-surface-2 hover:text-text',
                )}
              >
                <Icon aria-hidden="true" focusable="false" className="size-4 shrink-0" />
                <span className="min-w-0 truncate">{t(section.labelKey)}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

import { useEffect } from 'react';
import {
  BookOpen,
  Eraser,
  MessageSquarePlus,
  Paperclip,
  Sparkles,
  SquareTerminal,
  type LucideIcon,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { optionId, type PickerGroup, type PickerItem, type QuickCommand } from './skillPickerModel';
import { cn } from '@/lib/utils';

// SkillPicker (D-07/D-08): the PRESENTATIONAL ARIA listbox rendered above the composer input,
// built to the W3C WAI-ARIA APG combobox/listbox pattern. It is fully driven by props — 37D-04
// owns the open/active state and the input's aria-expanded/controls/activedescendant. Grouped
// role="option" rows (icon + name + optional subtitle, section headers per group); the active
// option carries aria-selected and is JS-scrolled into view (aria-activedescendant is NOT
// browser-auto-scrolled, Pitfall 6); DOM focus never enters the list (mousedown preventDefault
// keeps it on the input). Server strings render as auto-escaped React text nodes — zero
// HTML-injection surface (T-37D-05). Empty groups render nothing (degrade-to-no-op, D-09).

const COMMAND_ICON: Record<QuickCommand, LucideIcon> = {
  'add-files': Paperclip,
  'new-chat': MessageSquarePlus,
  clear: Eraser,
};

const SKILL_ICON_FOR_TYPE: Partial<Record<string, LucideIcon>> = {
  instruction: BookOpen,
  executable: SquareTerminal,
};

function iconFor(item: PickerItem): LucideIcon {
  if (item.kind === 'command') return COMMAND_ICON[item.command];
  return SKILL_ICON_FOR_TYPE[item.type] ?? Sparkles;
}

interface IndexedRow {
  readonly item: PickerItem;
  readonly id: string;
}

interface IndexedGroup {
  readonly headerKey: string;
  readonly rows: readonly IndexedRow[];
}

// Assign each option its stable GLOBAL index (index N = the Nth flattened option) so optionId
// + aria-activedescendant line up across groups. Computed purely (no mutable render-time
// counter, which react-hooks/immutability forbids) via a prefix sum of per-group item counts.
function indexGroups(groups: readonly PickerGroup[], listboxId: string): IndexedGroup[] {
  return groups.reduce<{ offset: number; out: IndexedGroup[] }>(
    (acc, group) => ({
      offset: acc.offset + group.items.length,
      out: [
        ...acc.out,
        {
          headerKey: group.headerKey,
          rows: group.items.map((item, i) => ({ item, id: optionId(listboxId, acc.offset + i) })),
        },
      ],
    }),
    { offset: 0, out: [] },
  ).out;
}

export interface SkillPickerProps {
  readonly groups: readonly PickerGroup[];
  readonly activeOptionId: string | undefined;
  readonly listboxId: string;
  readonly labelledById?: string;
  readonly onSelect: (item: PickerItem) => void;
}

export function SkillPicker({
  groups,
  activeOptionId,
  listboxId,
  labelledById,
  onSelect,
}: SkillPickerProps) {
  const { t } = useTranslation();

  useEffect(() => {
    if (activeOptionId === undefined) return;
    // aria-activedescendant does not auto-scroll (Pitfall 6): JS-scroll the active row.
    document.getElementById(activeOptionId)?.scrollIntoView({ block: 'nearest' });
  }, [activeOptionId]);

  const hasItems = groups.some((group) => group.items.length > 0);
  if (!hasItems) return null;

  const indexedGroups = indexGroups(groups, listboxId);
  return (
    <div className="absolute bottom-full left-0 z-50 mb-2 w-full min-w-[16rem] max-w-md overflow-hidden rounded-xl border border-border bg-surface-2 shadow-[var(--shadow-popover)]">
      <ul
        role="listbox"
        id={listboxId}
        aria-labelledby={labelledById}
        className="max-h-72 overflow-y-auto p-1.5"
      >
        {indexedGroups.map((group) => {
          const headerId = `${listboxId}-h-${group.headerKey}`;
          return (
            <li key={group.headerKey} role="group" aria-labelledby={headerId}>
              <span
                id={headerId}
                className="block px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-text-faint"
              >
                {t(group.headerKey)}
              </span>
              {group.rows.map(({ item, id }) => {
                const active = id === activeOptionId;
                const Icon = iconFor(item);
                const label = item.kind === 'skill' ? item.name : t(item.labelKey);
                const subtitle = item.kind === 'skill' ? item.description : t(item.subtitleKey);
                return (
                  <button
                    key={id}
                    id={id}
                    type="button"
                    role="option"
                    tabIndex={-1}
                    aria-selected={active}
                    onMouseDown={(event) => {
                      // Keep DOM focus on the composer input (APG combobox): stop the button
                      // from stealing focus; the click still selects.
                      event.preventDefault();
                    }}
                    onClick={() => {
                      onSelect(item);
                    }}
                    className={cn(
                      'flex w-full items-start gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors',
                      active ? 'bg-surface-3' : 'hover:bg-surface-3/60',
                    )}
                  >
                    <Icon
                      data-icon
                      aria-hidden="true"
                      className={cn(
                        'mt-0.5 size-4 shrink-0',
                        active ? 'text-accent-text' : 'text-text-faint',
                      )}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[13px] font-medium text-text">
                        {label}
                      </span>
                      {subtitle.length > 0 ? (
                        <span className="block truncate text-[11px] text-text-muted">
                          {subtitle}
                        </span>
                      ) : null}
                    </span>
                  </button>
                );
              })}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

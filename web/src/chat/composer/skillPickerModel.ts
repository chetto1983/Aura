// skillPickerModel.ts — the PURE combobox decision core for the '/' skill picker
// (WEBSKILL-01/03). It is React/DOM-free on purpose: the trigger predicate (D-05/D-09),
// the incremental filter+group, the wrap-around active-index math, and the key→action
// mapping all live here so they are the unit-test + Stryker-mutation target that protects
// the ≥85% web coverage floor. The presentational SkillPicker.tsx consumes PickerGroup[] +
// optionId and owns no decision logic.
import type { ComposerSkillRow } from './api';

/** The three net-new client-side quick commands (D-02): add-files reuses the Paperclip
 * handler, new-chat reuses startNewConversation, clear is a pure client reset — all wired
 * by the Composer in 37D-04. The picker only surfaces + selects them. */
export type QuickCommand = 'add-files' | 'new-chat' | 'clear';

/** A selectable skill row (icon + name + optional description subtitle). */
export interface SkillPickerItem {
  readonly kind: 'skill';
  readonly name: string;
  readonly description: string;
  readonly type: string;
}

/** A selectable quick command; labelKey/subtitleKey are i18n keys the presentational layer
 * localizes (the pure model never calls t()). */
export interface CommandPickerItem {
  readonly kind: 'command';
  readonly command: QuickCommand;
  readonly labelKey: string;
  readonly subtitleKey: string;
}

export type PickerItem = SkillPickerItem | CommandPickerItem;

/** One grouped section: a localized header (i18n key) + its items. */
export interface PickerGroup {
  readonly headerKey: string;
  readonly items: readonly PickerItem[];
}

const KEY_PREFIX = 'chat.skillPicker';
export const COMMANDS_HEADER_KEY = `${KEY_PREFIX}.commandsHeader`;
export const SKILLS_HEADER_KEY = `${KEY_PREFIX}.skillsHeader`;

interface QuickCommandSpec {
  readonly command: QuickCommand;
  readonly labelKey: string;
  readonly subtitleKey: string;
}

// The command catalog. `command` doubles as the filter corpus (e.g. 'add-files' matches
// 'add' or 'files', 'new-chat' matches 'new' or 'chat') so the pure model filters commands
// without resolving i18n — its identifier mirrors the localized label's intent.
const QUICK_COMMANDS: readonly QuickCommandSpec[] = [
  {
    command: 'add-files',
    labelKey: `${KEY_PREFIX}.cmdAddFiles`,
    subtitleKey: `${KEY_PREFIX}.cmdAddFilesSubtitle`,
  },
  {
    command: 'new-chat',
    labelKey: `${KEY_PREFIX}.cmdNewChat`,
    subtitleKey: `${KEY_PREFIX}.cmdNewChatSubtitle`,
  },
  {
    command: 'clear',
    labelKey: `${KEY_PREFIX}.cmdClear`,
    subtitleKey: `${KEY_PREFIX}.cmdClearSubtitle`,
  },
];

/** D-05 trigger + D-09 degrade: the menu opens only when the composer text starts with '/'
 * (whole-content, so a mid-text slash stays literal) AND the skills list is non-empty (an
 * empty/unreachable list ⇒ the picker never opens). */
export function shouldOpen(text: string, skillsCount: number): boolean {
  return text.startsWith('/') && skillsCount > 0;
}

/** The filter query is the composer text after the leading '/' (empty when no slash). */
export function pickerFilter(text: string): string {
  return text.startsWith('/') ? text.slice(1) : '';
}

function commandMatches(spec: QuickCommandSpec, needle: string): boolean {
  return needle === '' || spec.command.toLowerCase().includes(needle);
}

function skillMatches(skill: ComposerSkillRow, needle: string): boolean {
  if (needle === '') return true;
  return (
    skill.name.toLowerCase().includes(needle) || skill.description.toLowerCase().includes(needle)
  );
}

/** Build the grouped, incrementally-filtered picker items: a Commands group (add-files/
 * new-chat/clear) + a Skills group covering every matching skill. The filter is
 * case-insensitive over the skill name+description (and the command id); empty groups are
 * dropped so a no-match filter collapses the menu. */
export function filterPickerItems(
  skills: readonly ComposerSkillRow[],
  filter: string,
): PickerGroup[] {
  const needle = filter.trim().toLowerCase();
  const commandItems = QUICK_COMMANDS.filter((spec) => commandMatches(spec, needle)).map(
    (spec): PickerItem => ({
      kind: 'command',
      command: spec.command,
      labelKey: spec.labelKey,
      subtitleKey: spec.subtitleKey,
    }),
  );
  const skillItems = skills
    .filter((skill) => skillMatches(skill, needle))
    .map(
      (skill): PickerItem => ({
        kind: 'skill',
        name: skill.name,
        description: skill.description,
        type: skill.type,
      }),
    );
  const groups: PickerGroup[] = [];
  if (commandItems.length > 0) groups.push({ headerKey: COMMANDS_HEADER_KEY, items: commandItems });
  if (skillItems.length > 0) groups.push({ headerKey: SKILLS_HEADER_KEY, items: skillItems });
  return groups;
}

/** Flatten grouped items into linear option order (index N = the Nth option) so the
 * active-index math + optionId line up with the rendered rows. */
export function flattenItems(groups: readonly PickerGroup[]): PickerItem[] {
  return groups.flatMap((group) => [...group.items]);
}

/** Wrap-around active-option index: stepping past either end rolls to the other side, so ↑
 * from the first option lands on the last and ↓ from the last lands on the first. Returns
 * -1 for an empty list (no active option). */
export function nextActiveIndex(current: number, delta: number, len: number): number {
  if (len <= 0) return -1;
  return (((current + delta) % len) + len) % len;
}

/** A stable, unique DOM id per option index for aria-activedescendant + scroll-into-view. */
export function optionId(baseId: string, index: number): string {
  return `${baseId}-opt-${String(index)}`;
}

/** Map a keydown to the picker action; every non-navigation key is 'none' so the Composer
 * only preventDefault's the four navigation keys while the menu is open (D-09: Enter-send
 * and literal typing stay intact when the menu is closed). */
export function pickerKeyAction(key: string): 'up' | 'down' | 'select' | 'close' | 'none' {
  switch (key) {
    case 'ArrowUp':
      return 'up';
    case 'ArrowDown':
      return 'down';
    case 'Enter':
      return 'select';
    case 'Escape':
      return 'close';
    default:
      return 'none';
  }
}

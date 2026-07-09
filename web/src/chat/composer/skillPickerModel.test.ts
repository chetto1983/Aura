import { describe, expect, it } from 'vitest';
import type { ComposerSkillRow } from './api';
import {
  COMMANDS_HEADER_KEY,
  SKILLS_HEADER_KEY,
  filterPickerItems,
  flattenItems,
  nextActiveIndex,
  optionId,
  pickerFilter,
  pickerKeyAction,
  shouldOpen,
  type PickerItem,
} from './skillPickerModel';

// skillPickerModel.test — every decision the '/' picker makes lives in the pure model, so
// this is the coverage/mutation target: the D-05 trigger, the incremental filter+group, the
// wrap-around index math, the stable option id, and the key→action mapping.

const SKILLS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
  { name: 'docx-writer', description: 'Write Word documents', type: 'executable' },
  { name: 'graph-explorer', description: 'Explore the knowledge graph', type: 'instruction' },
];

function skillNames(items: readonly PickerItem[]): string[] {
  return items.flatMap((item) => (item.kind === 'skill' ? [item.name] : []));
}

function commandIds(items: readonly PickerItem[]): string[] {
  return items.flatMap((item) => (item.kind === 'command' ? [item.command] : []));
}

describe('shouldOpen (D-05 trigger + D-09 degrade)', () => {
  it('opens on a leading slash when skills exist', () => {
    expect(shouldOpen('/', 3)).toBe(true);
    expect(shouldOpen('/foo', 3)).toBe(true);
  });

  it('stays closed for empty text or a mid-text literal slash', () => {
    expect(shouldOpen('', 3)).toBe(false);
    expect(shouldOpen('a/b', 3)).toBe(false);
  });

  it('never opens when the skills list is empty (D-09)', () => {
    expect(shouldOpen('/', 0)).toBe(false);
  });
});

describe('pickerFilter', () => {
  it('returns the text after the leading slash, else empty', () => {
    expect(pickerFilter('/cre')).toBe('cre');
    expect(pickerFilter('/')).toBe('');
    expect(pickerFilter('')).toBe('');
    expect(pickerFilter('abc')).toBe('');
  });
});

describe('filterPickerItems', () => {
  it('empty filter → a Commands group (add-files/new-chat/clear) + a Skills group of all skills', () => {
    const groups = filterPickerItems(SKILLS, '');

    expect(groups).toHaveLength(2);
    expect(groups[0]?.headerKey).toBe(COMMANDS_HEADER_KEY);
    expect(commandIds(groups[0]?.items ?? [])).toEqual(['add-files', 'new-chat', 'clear']);
    expect(groups[1]?.headerKey).toBe(SKILLS_HEADER_KEY);
    expect(skillNames(groups[1]?.items ?? [])).toEqual([
      'skill-creator',
      'docx-writer',
      'graph-explorer',
    ]);
  });

  it('keeps only case-insensitive name/description matches and drops the empty commands group', () => {
    const groups = filterPickerItems(SKILLS, 'cre');

    // 'cre' matches skill-creator (name + "Create") but no command id → commands group dropped.
    expect(groups).toHaveLength(1);
    expect(groups[0]?.headerKey).toBe(SKILLS_HEADER_KEY);
    expect(skillNames(groups[0]?.items ?? [])).toEqual(['skill-creator']);
  });

  it('matches a command by its id and drops the empty skills group', () => {
    const groups = filterPickerItems(SKILLS, 'files');

    // 'files' matches the add-files command; no skill name/description contains it.
    expect(groups).toHaveLength(1);
    expect(groups[0]?.headerKey).toBe(COMMANDS_HEADER_KEY);
    expect(commandIds(groups[0]?.items ?? [])).toEqual(['add-files']);
  });

  it('matches both a command and a skill on the same token', () => {
    const groups = filterPickerItems(SKILLS, 'new');

    // 'new' matches new-chat (command id) AND skill-creator ("Create a new skill").
    expect(commandIds(groups[0]?.items ?? [])).toEqual(['new-chat']);
    expect(skillNames(groups[1]?.items ?? [])).toEqual(['skill-creator']);
  });

  it('collapses to no groups when nothing matches', () => {
    expect(filterPickerItems(SKILLS, 'zzz-nope')).toEqual([]);
  });
});

describe('flattenItems', () => {
  it('linearizes commands-then-skills in render order', () => {
    const flat = flattenItems(filterPickerItems(SKILLS, ''));
    expect(flat).toHaveLength(6);
    expect(flat[0]?.kind).toBe('command');
    expect(flat[3]?.kind).toBe('skill');
  });
});

describe('nextActiveIndex (wrap-around)', () => {
  it('wraps up past the first option to the last', () => {
    expect(nextActiveIndex(0, -1, 5)).toBe(4);
  });

  it('wraps down past the last option to the first', () => {
    expect(nextActiveIndex(4, 1, 5)).toBe(0);
  });

  it('moves from -1 (no active) to the first option on a down step', () => {
    expect(nextActiveIndex(-1, 1, 5)).toBe(0);
  });

  it('returns -1 for an empty list', () => {
    expect(nextActiveIndex(0, 1, 0)).toBe(-1);
  });
});

describe('optionId', () => {
  it('is stable and unique per index', () => {
    expect(optionId('skpick', 2)).toBe('skpick-opt-2');
    expect(optionId('skpick', 0)).not.toBe(optionId('skpick', 1));
  });
});

describe('pickerKeyAction', () => {
  it('maps the four navigation keys and nothing else', () => {
    expect(pickerKeyAction('ArrowDown')).toBe('down');
    expect(pickerKeyAction('ArrowUp')).toBe('up');
    expect(pickerKeyAction('Enter')).toBe('select');
    expect(pickerKeyAction('Escape')).toBe('close');
    expect(pickerKeyAction('a')).toBe('none');
  });
});

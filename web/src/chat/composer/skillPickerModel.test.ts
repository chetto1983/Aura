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
  resolveSlashSubmission,
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

  // The command name ends at the first space; everything after it is the instruction the
  // operator is writing for the skill, not more filter text. Without this, '/pdf estrai le
  // tabelle' filters on the whole sentence and matches nothing.
  it('stops at the first space so the instruction is not part of the filter', () => {
    expect(pickerFilter('/pdf estrai le tabelle')).toBe('pdf');
  });
});

describe('resolveSlashSubmission', () => {
  // The operator's words: the skill belongs IN the message, not on a chip above it. So the
  // text is sent verbatim — '/pdf estrai le tabelle' is what the transcript shows — and the
  // skill name rides beside it for the server to resolve into the model-facing body.
  it('names the skill and leaves the message exactly as typed', () => {
    expect(resolveSlashSubmission('/docx-writer scrivi il verbale', SKILLS)).toEqual({
      skill: 'docx-writer',
      text: '/docx-writer scrivi il verbale',
    });
  });

  it('starts a skill with no instruction at all', () => {
    expect(resolveSlashSubmission('/docx-writer', SKILLS)).toEqual({
      skill: 'docx-writer',
      text: '/docx-writer',
    });
  });

  it('matches the skill name case-insensitively', () => {
    expect(resolveSlashSubmission('/DOCX-Writer ciao', SKILLS).skill).toBe('docx-writer');
  });

  it('leaves ordinary text alone', () => {
    expect(resolveSlashSubmission('quanto fa 2+2', SKILLS)).toEqual({
      skill: null,
      text: 'quanto fa 2+2',
    });
  });

  // An unknown slash word is a typo, not a skill: sending it as a plain message is what
  // lets the operator see their own mistake instead of it silently becoming a normal turn
  // with an invented skill attached.
  it('does not invent a skill for an unknown slash word', () => {
    expect(resolveSlashSubmission('/nope do something', SKILLS)).toEqual({
      skill: null,
      text: '/nope do something',
    });
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

  // Measured live on the deployment 2026-08-17: typing '/pdf' listed docx first (its
  // description says "Do NOT use for PDFs") and Enter pinned DOCX. Matching the
  // description is right — it is how you find a skill whose name you do not know — but it
  // must never outrank the skill the operator actually named.
  it('ranks a name match above a description-only match', () => {
    const skills: readonly ComposerSkillRow[] = [
      { name: 'docx', description: 'Do NOT use for PDFs, spreadsheets', type: 'instruction' },
      { name: 'find-skills', description: 'file formats (xlsx, pdf, docx)', type: 'instruction' },
      { name: 'pdf', description: 'Anything with PDF files', type: 'instruction' },
    ];

    const groups = filterPickerItems(skills, 'pdf');

    // All three still match — nothing is hidden — but the named one is the one Enter takes.
    expect(skillNames(groups[0]?.items ?? [])).toEqual(['pdf', 'docx', 'find-skills']);
  });

  it('ranks an exact name above a prefix, and a prefix above a substring', () => {
    const skills: readonly ComposerSkillRow[] = [
      { name: 'my-pdf-tool', description: '', type: 'instruction' },
      { name: 'pdf-writer', description: '', type: 'instruction' },
      { name: 'pdf', description: '', type: 'instruction' },
    ];

    expect(skillNames(filterPickerItems(skills, 'pdf')[0]?.items ?? [])).toEqual([
      'pdf',
      'pdf-writer',
      'my-pdf-tool',
    ]);
  });

  it('keeps the source order within one rank (stable, not alphabetised)', () => {
    const skills: readonly ComposerSkillRow[] = [
      { name: 'zeta-pdf', description: '', type: 'instruction' },
      { name: 'alpha-pdf', description: '', type: 'instruction' },
    ];

    expect(skillNames(filterPickerItems(skills, 'pdf')[0]?.items ?? [])).toEqual([
      'zeta-pdf',
      'alpha-pdf',
    ]);
  });

  it('ranks commands the same way, so /clear takes clear and not add-files', () => {
    // 'clear' is a substring of nothing else, but the ordering rule must hold for the
    // command group too: the exact id comes first whatever its position in the catalog.
    expect(commandIds(filterPickerItems(SKILLS, 'clear')[0]?.items ?? [])).toEqual(['clear']);
    expect(commandIds(filterPickerItems(SKILLS, 'chat')[0]?.items ?? [])).toEqual(['new-chat']);
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

import { describe, expect, it } from 'vitest';
import type { Unstable_TriggerItem } from '@assistant-ui/react';
import type { ComposerSkillRow } from './api';
import { COMMAND_ITEM_TYPE } from './commands';
import {
  createSkillDirectiveFormatter,
  createSkillTriggerAdapter,
  rankTriggerItems,
  resolveSlashSubmission,
} from './skillTrigger';

// skillTrigger.test — the two things Aura contributes to the library's '/' popover: which
// items exist (ranked) and how a chosen one is written into the message. Detection, the
// open/close lifecycle, keyboard navigation and ARIA belong to
// ComposerPrimitive.Unstable_TriggerPopover and are NOT retested here.

const SKILLS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
  { name: 'docx-writer', description: 'Write Word documents', type: 'executable' },
  { name: 'graph-explorer', description: 'Explore the knowledge graph', type: 'instruction' },
];

function names(items: readonly { id: string }[]): string[] {
  return items.map((item) => item.id);
}

/** The trigger items the adapter builds from skills, so a ranking test can go through the
 * same projection the popover sees rather than a hand-written shape beside it. */
function itemsFor(skills: readonly ComposerSkillRow[]): readonly Unstable_TriggerItem[] {
  return createSkillTriggerAdapter(skills).search?.('') ?? [];
}

function rankSkills(skills: readonly ComposerSkillRow[], query: string) {
  return rankTriggerItems(itemsFor(skills), query);
}

const COMPACT_ITEM: Unstable_TriggerItem = {
  id: 'compact',
  type: COMMAND_ITEM_TYPE,
  label: 'Compact',
  description: 'Condense the earlier turns into a summary',
};

describe('rankSkills', () => {
  it('returns every skill, in source order, for an empty query', () => {
    expect(names(rankSkills(SKILLS, ''))).toEqual([
      'skill-creator',
      'docx-writer',
      'graph-explorer',
    ]);
  });

  // Measured live 2026-08-17: '/pdf' listed docx first (its description says "Do NOT use for
  // PDFs") and Enter took the first row, so the operator got docx. Description matches must
  // still appear — that is how you find a skill whose name you do not know — but below the
  // one that was actually named.
  it('ranks a name match above a description-only match', () => {
    const skills: readonly ComposerSkillRow[] = [
      { name: 'docx', description: 'Do NOT use for PDFs, spreadsheets', type: 'instruction' },
      { name: 'find-skills', description: 'file formats (xlsx, pdf, docx)', type: 'instruction' },
      { name: 'pdf', description: 'Anything with PDF files', type: 'instruction' },
    ];

    expect(names(rankSkills(skills, 'pdf'))).toEqual(['pdf', 'docx', 'find-skills']);
  });

  it('ranks exact above prefix above substring, and keeps source order within a rank', () => {
    const skills: readonly ComposerSkillRow[] = [
      { name: 'my-pdf-tool', description: '', type: 'instruction' },
      { name: 'zeta-pdf', description: '', type: 'instruction' },
      { name: 'pdf-writer', description: '', type: 'instruction' },
      { name: 'pdf', description: '', type: 'instruction' },
    ];

    expect(names(rankSkills(skills, 'pdf'))).toEqual([
      'pdf',
      'pdf-writer',
      'my-pdf-tool',
      'zeta-pdf',
    ]);
  });

  it('drops non-matches entirely', () => {
    expect(rankSkills(SKILLS, 'zzz-nope')).toEqual([]);
  });

  it('projects a skill onto the trigger-item shape the popover renders', () => {
    const [item] = rankSkills(SKILLS, 'docx-writer');

    expect(item).toEqual({
      id: 'docx-writer',
      type: 'skill',
      label: 'docx-writer',
      description: 'Write Word documents',
      metadata: { skillType: 'executable' },
    });
  });
});

describe('createSkillTriggerAdapter', () => {
  // The empty category list is load-bearing, not an omission: the navigation resource shows
  // categories INSTEAD of items when the query is empty and any category exists, which would
  // put a one-entry "Skills" drill-down in front of every bare '/'.
  it('reports no categories so a bare slash lists skills immediately', () => {
    const adapter = createSkillTriggerAdapter(SKILLS);

    expect(adapter.categories()).toEqual([]);
    expect(adapter.categoryItems('skills')).toEqual([]);
    expect(names(adapter.search?.('') ?? [])).toHaveLength(3);
  });

  it('searches through the ranking', () => {
    const adapter = createSkillTriggerAdapter(SKILLS);

    expect(names(adapter.search?.('creat') ?? [])).toEqual(['skill-creator']);
  });
});

describe('createSkillDirectiveFormatter', () => {
  const formatter = createSkillDirectiveFormatter(SKILLS);

  // The library's default emits `:skill[pdf]`; Aura's composer is a plain textarea and the
  // operator asked for the skill to live in the message, so it serializes to what they would
  // have typed. NO trailing space: triggerSelectionResource appends the separator itself,
  // and a second one showed up live as '/pdf  estrai'.
  it('writes the command the operator would have typed, without its own separator', () => {
    expect(formatter.serialize({ id: 'docx-writer', type: 'skill', label: 'docx-writer' })).toBe(
      '/docx-writer',
    );
  });

  it('parses a leading installed skill into a mention plus the remaining instruction', () => {
    expect(formatter.parse('/docx-writer scrivi il verbale')).toEqual([
      { kind: 'mention', type: 'skill', label: 'docx-writer', id: 'docx-writer' },
      { kind: 'text', text: ' scrivi il verbale' },
    ]);
  });

  it('parses a bare installed skill into a single mention', () => {
    expect(formatter.parse('/docx-writer')).toEqual([
      { kind: 'mention', type: 'skill', label: 'docx-writer', id: 'docx-writer' },
    ]);
  });

  it('leaves an unknown slash word as plain text rather than inventing a skill', () => {
    expect(formatter.parse('/nope do something')).toEqual([
      { kind: 'text', text: '/nope do something' },
    ]);
  });

  it('leaves ordinary prose alone', () => {
    expect(formatter.parse('quanto fa 2+2')).toEqual([{ kind: 'text', text: 'quanto fa 2+2' }]);
  });
});

describe('resolveSlashSubmission', () => {
  it('names the skill and leaves the message exactly as typed', () => {
    expect(resolveSlashSubmission('/docx-writer scrivi il verbale', SKILLS)).toEqual({
      skill: 'docx-writer',
      command: null,
      text: '/docx-writer scrivi il verbale',
    });
  });

  it('starts a skill invoked with no instruction at all', () => {
    expect(resolveSlashSubmission('/docx-writer', SKILLS)).toEqual({
      skill: 'docx-writer',
      command: null,
      text: '/docx-writer',
    });
  });

  it('matches the skill name case-insensitively', () => {
    expect(resolveSlashSubmission('/DOCX-Writer ciao', SKILLS).skill).toBe('docx-writer');
  });

  it('leaves ordinary text alone', () => {
    expect(resolveSlashSubmission('quanto fa 2+2', SKILLS)).toEqual({
      skill: null,
      command: null,
      text: 'quanto fa 2+2',
    });
  });

  // A command is not a message: the caller runs it and sends nothing. This is the path a
  // typed-out '/compact' + Enter takes when the menu was dismissed.
  it('names a command instead of a skill', () => {
    expect(resolveSlashSubmission('/compact', SKILLS)).toEqual({
      skill: null,
      command: 'compact',
      text: '/compact',
    });
  });

  it('does not invent a skill for an unknown slash word', () => {
    expect(resolveSlashSubmission('/nope do something', SKILLS)).toEqual({
      skill: null,
      command: null,
      text: '/nope do something',
    });
  });
});

describe('commands in the menu', () => {
  it('lists commands before skills for a bare slash', () => {
    const adapter = createSkillTriggerAdapter(SKILLS, [COMPACT_ITEM]);

    expect(names(adapter.search?.('') ?? [])[0]).toBe('compact');
  });

  // The word that INVOKES a command is its id, not its translated label, so '/comp' has to
  // find it in either language while '/compact' stays an exact hit.
  it('matches a command on its invoking word and on its label', () => {
    const adapter = createSkillTriggerAdapter(SKILLS, [
      { ...COMPACT_ITEM, label: 'Compatta', description: 'Condensa i turni precedenti' },
    ]);

    expect(names(adapter.search?.('compact') ?? [])).toEqual(['compact']);
    expect(names(adapter.search?.('compat') ?? [])).toEqual(['compact']);
  });

  it('parses a leading command into a mention of its own type', () => {
    expect(createSkillDirectiveFormatter(SKILLS).parse('/compact')).toEqual([
      { kind: 'mention', type: COMMAND_ITEM_TYPE, label: 'compact', id: 'compact' },
    ]);
  });

  // A skill cannot take a verb the composer already owns, or '/compact' would become a turn.
  it('resolves a command ahead of a skill with the same name', () => {
    const shadowing: readonly ComposerSkillRow[] = [
      { name: 'compact', description: 'a skill that shadows the command', type: 'instruction' },
    ];

    expect(resolveSlashSubmission('/compact', shadowing)).toEqual({
      skill: null,
      command: 'compact',
      text: '/compact',
    });
  });
});

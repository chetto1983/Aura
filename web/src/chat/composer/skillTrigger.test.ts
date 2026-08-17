import { describe, expect, it } from 'vitest';
import type { ComposerSkillRow } from './api';
import {
  createSkillDirectiveFormatter,
  createSkillTriggerAdapter,
  rankSkills,
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
      text: '/docx-writer scrivi il verbale',
    });
  });

  it('starts a skill invoked with no instruction at all', () => {
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

  it('does not invent a skill for an unknown slash word', () => {
    expect(resolveSlashSubmission('/nope do something', SKILLS)).toEqual({
      skill: null,
      text: '/nope do something',
    });
  });
});

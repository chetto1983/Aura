// The adapter and formatter contracts live in @assistant-ui/core — TriggerPopover's own
// `adapter` prop is typed against it — while the item type is re-exported by react.
import type { Unstable_DirectiveFormatter, Unstable_TriggerAdapter } from '@assistant-ui/core';
import type { Unstable_TriggerItem } from '@assistant-ui/react';
import type { ComposerSkillRow } from './api';

// skillTrigger — the '/' skill menu expressed as the library's own extension points instead
// of a hand-rolled listbox. ComposerPrimitive.Unstable_TriggerPopover owns detection, the
// open/close lifecycle, keyboard navigation and ARIA; this file supplies only the two things
// the library cannot know: WHICH items exist (the adapter) and how a chosen one is written
// into the message (the formatter).
//
// Aura had ~330 LOC of picker doing the library's half as well — active-index math with
// wrap-around, an Escape latch, a key→action map — while @assistant-ui/react shipped all of
// it. That duplication is what let '/pdf' pin docx: the bug lived in the half we should
// never have written.

export const SKILL_TRIGGER_CHAR = '/';

/** The trigger item `type` every skill carries; the formatter keys off it. */
export const SKILL_ITEM_TYPE = 'skill';

// Relevance ranks, lowest wins. A description match is a real match — it is how you find a
// skill whose name you do not know — but it must never outrank the skill the operator NAMED.
// Measured live 2026-08-17: '/pdf' listed docx first (its description reads "Do NOT use for
// PDFs") and Enter took the first row.
const RANK_EXACT_NAME = 0;
const RANK_NAME_PREFIX = 1;
const RANK_NAME_SUBSTRING = 2;
const RANK_DESCRIPTION_ONLY = 3;
const RANK_NO_MATCH = -1;

function nameRank(name: string, needle: string): number {
  const candidate = name.toLowerCase();
  if (candidate === needle) return RANK_EXACT_NAME;
  if (candidate.startsWith(needle)) return RANK_NAME_PREFIX;
  if (candidate.includes(needle)) return RANK_NAME_SUBSTRING;
  return RANK_NO_MATCH;
}

function skillRank(skill: ComposerSkillRow, needle: string): number {
  if (needle === '') return RANK_EXACT_NAME;
  const byName = nameRank(skill.name, needle);
  if (byName !== RANK_NO_MATCH) return byName;
  return skill.description.toLowerCase().includes(needle) ? RANK_DESCRIPTION_ONLY : RANK_NO_MATCH;
}

function toTriggerItem(skill: ComposerSkillRow): Unstable_TriggerItem {
  return {
    id: skill.name,
    type: SKILL_ITEM_TYPE,
    label: skill.name,
    description: skill.description,
    metadata: { skillType: skill.type },
  };
}

/** Rank, drop the non-matches, keep source order within a rank (Array#sort is stable). */
export function rankSkills(
  skills: readonly ComposerSkillRow[],
  query: string,
): readonly Unstable_TriggerItem[] {
  const needle = query.trim().toLowerCase();
  return skills
    .map((skill) => ({ skill, rank: skillRank(skill, needle) }))
    .filter((row) => row.rank !== RANK_NO_MATCH)
    .sort((a, b) => a.rank - b.rank)
    .map((row) => toTriggerItem(row.skill));
}

/** The adapter the popover reads.
 *
 * It reports NO categories on purpose. The navigation resource shows the category list
 * instead of items whenever the query is empty AND categories exist
 * (triggerNavigationResource: `if (!query && categories.length > 0) return null`), which
 * would put a pointless "Skills" drill-down in front of every bare '/'. With none, an empty
 * query goes straight to `search('')` and the operator sees the skills immediately. */
export function createSkillTriggerAdapter(
  skills: readonly ComposerSkillRow[],
): Unstable_TriggerAdapter {
  return {
    categories: () => [],
    categoryItems: () => [],
    search: (query) => rankSkills(skills, query),
  };
}

/** How a chosen skill is written into the message.
 *
 * The default formatter emits `:skill[pdf]` (MDX directive syntax). Aura's composer is a
 * plain textarea and the operator asked for the skill to live in the message itself, so the
 * serialization the transcript will show is the command the operator would have typed:
 * `/pdf`. No trailing space: the selection resource already appends one when the text after
 * the trigger does not start with one (triggerSelectionResource.js:34), and adding a second
 * gave `/pdf  estrai` — measured live before this was removed.
 *
 * `parse` recognises a leading `/name` ONLY when the name is installed, so a typo stays
 * plain text the operator can see rather than a chip that lies about being a skill. */
export function createSkillDirectiveFormatter(
  skills: readonly ComposerSkillRow[],
): Unstable_DirectiveFormatter {
  return {
    serialize: (item) => `${SKILL_TRIGGER_CHAR}${item.id}`,
    parse: (text) => {
      const match = /^\/([^\s]+)(\s[\s\S]*)?$/.exec(text);
      const name = match?.[1];
      const skill = name === undefined ? undefined : findSkill(skills, name);
      if (match === null || skill === undefined) return [{ kind: 'text', text }];
      const rest = match[2] ?? '';
      const mention = {
        kind: 'mention' as const,
        type: SKILL_ITEM_TYPE,
        label: skill.name,
        id: skill.name,
      };
      return rest === '' ? [mention] : [mention, { kind: 'text', text: rest }];
    },
  };
}

function findSkill(
  skills: readonly ComposerSkillRow[],
  name: string,
): ComposerSkillRow | undefined {
  const needle = name.toLowerCase();
  return skills.find((skill) => skill.name.toLowerCase() === needle);
}

/** What a composer text submits as: the message exactly as typed, plus the skill it names.
 *
 * The skill rides BESIDE the text rather than replacing it — '/pdf estrai le tabelle' is
 * what the transcript shows — and the server turns the name into the model-facing body
 * (server_run.go: UseAuthorityFrame + SkillBody + the user text). */
export function resolveSlashSubmission(
  text: string,
  skills: readonly ComposerSkillRow[],
): { skill: string | null; text: string } {
  if (!text.startsWith(SKILL_TRIGGER_CHAR)) return { skill: null, text };
  const word = text.slice(1).split(' ')[0] ?? '';
  if (word === '') return { skill: null, text };
  // Exact name only. A leading '/word' that names nothing installed is a typo, and sending
  // it verbatim shows the operator their own mistake instead of quietly attaching some
  // near-miss skill to an ordinary turn.
  return { skill: findSkill(skills, word)?.name ?? null, text };
}

// The adapter and formatter contracts live in @assistant-ui/core — TriggerPopover's own
// `adapter` prop is typed against it — while the item type is re-exported by react.
import type { Unstable_DirectiveFormatter, Unstable_TriggerAdapter } from '@assistant-ui/core';
import type { Unstable_TriggerItem } from '@assistant-ui/react';
import { COMMAND_ITEM_TYPE, findCommand } from './commands';
import type { ComposerSkillRow } from './api';

// skillTrigger — the '/' menu expressed as the library's own extension points instead of a
// hand-rolled listbox. ComposerPrimitive.Unstable_TriggerPopover owns detection, the
// open/close lifecycle, keyboard navigation and ARIA; this file supplies only the two things
// the library cannot know: WHICH items exist (the adapter) and how a chosen one is written
// into the message (the formatter).
//
// Aura had ~330 LOC of picker doing the library's half as well — active-index math with
// wrap-around, an Escape latch, a key→action map — while @assistant-ui/react shipped all of
// it. That duplication is what let '/pdf' pin docx: the bug lived in the half we should
// never have written.
//
// The menu lists SKILLS and COMMANDS. They rank against one query on the same scale, and the
// difference between them is what happens on selection, not how they are found — see
// commands.ts.

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

/** How well one menu row answers the query. A command is matched on the word that INVOKES it
 * (its id) as well as on its translated label, so '/comp' finds Compact in either language
 * while '/compact' stays an exact hit. */
function itemRank(item: Unstable_TriggerItem, needle: string): number {
  if (needle === '') return RANK_EXACT_NAME;
  for (const name of [item.id, item.label]) {
    const rank = nameRank(name, needle);
    if (rank !== RANK_NO_MATCH) return rank;
  }
  return (item.description ?? '').toLowerCase().includes(needle)
    ? RANK_DESCRIPTION_ONLY
    : RANK_NO_MATCH;
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

/** Rank, drop the non-matches, keep source order within a rank (Array#sort is stable).
 *
 * Commands are passed first by the adapter, so a bare '/' — where every row ties at
 * RANK_EXACT_NAME — opens with them at the top, above however many skills are installed. */
export function rankTriggerItems(
  items: readonly Unstable_TriggerItem[],
  query: string,
): readonly Unstable_TriggerItem[] {
  const needle = query.trim().toLowerCase();
  return items
    .map((item) => ({ item, rank: itemRank(item, needle) }))
    .filter((row) => row.rank !== RANK_NO_MATCH)
    .sort((a, b) => a.rank - b.rank)
    .map((row) => row.item);
}

/** The adapter the popover reads.
 *
 * It reports NO categories on purpose. The navigation resource shows the category list
 * instead of items whenever the query is empty AND categories exist
 * (triggerNavigationResource: `if (!query && categories.length > 0) return null`), which
 * would put a pointless drill-down in front of every bare '/'. With none, an empty query
 * goes straight to `search('')` and the operator sees the menu immediately. */
export function createSkillTriggerAdapter(
  skills: readonly ComposerSkillRow[],
  commands: readonly Unstable_TriggerItem[] = [],
): Unstable_TriggerAdapter {
  const items = [...commands, ...skills.map(toTriggerItem)];
  return {
    categories: () => [],
    categoryItems: () => [],
    search: (query) => rankTriggerItems(items, query),
  };
}

/** How a chosen row is written into the message.
 *
 * The default formatter emits `:skill[pdf]` (MDX directive syntax). Aura's composer is a
 * plain textarea and the operator asked for the skill to live in the message itself, so the
 * serialization the transcript will show is the command the operator would have typed:
 * `/pdf`. No trailing space: the selection resource already appends one when the text after
 * the trigger does not start with one (triggerSelectionResource.js:34), and adding a second
 * gave `/pdf  estrai` — measured live before this was removed.
 *
 * A COMMAND is serialized the same way and then cleared by the executor, because the
 * TriggerPopover.Action behavior inserts the directive BEFORE calling onExecute and
 * `removeOnExecute` is a property of the behavior rather than of the item — one flag cannot
 * describe both halves of a menu where skills stay in the text and commands do not.
 *
 * `parse` recognises a leading `/name` ONLY when the name is installed, so a typo stays plain
 * text the operator can see rather than a chip that lies about being a skill. */
export function createSkillDirectiveFormatter(
  skills: readonly ComposerSkillRow[],
): Unstable_DirectiveFormatter {
  return {
    serialize: (item) => `${SKILL_TRIGGER_CHAR}${item.id}`,
    parse: (text) => {
      const match = /^\/([^\s]+)(\s[\s\S]*)?$/.exec(text);
      const name = match?.[1];
      const known = name === undefined ? undefined : resolveSlashWord(name, skills);
      if (match === null || known === undefined) return [{ kind: 'text', text }];
      const rest = match[2] ?? '';
      const mention = { kind: 'mention' as const, type: known.type, label: known.id, id: known.id };
      return rest === '' ? [mention] : [mention, { kind: 'text', text: rest }];
    },
  };
}

/** What a leading `/word` names, if anything: a command, an installed skill, or neither.
 * Commands win a name collision — a skill cannot take a verb the composer already owns. */
function resolveSlashWord(
  word: string,
  skills: readonly ComposerSkillRow[],
): { readonly id: string; readonly type: string } | undefined {
  const command = findCommand(word);
  if (command !== undefined) return { id: command.name, type: COMMAND_ITEM_TYPE };
  const skill = findSkill(skills, word);
  return skill === undefined ? undefined : { id: skill.name, type: SKILL_ITEM_TYPE };
}

function findSkill(
  skills: readonly ComposerSkillRow[],
  name: string,
): ComposerSkillRow | undefined {
  const needle = name.toLowerCase();
  return skills.find((skill) => skill.name.toLowerCase() === needle);
}

/** What a composer text submits as: the message exactly as typed, plus what its leading
 * `/word` named.
 *
 * A skill rides BESIDE the text rather than replacing it — '/pdf estrai le tabelle' is what
 * the transcript shows — and the server turns the name into the model-facing body
 * (server_run.go: UseAuthorityFrame + SkillBody + the user text).
 *
 * A command is the other case: it is not a message at all, so the caller runs it and sends
 * nothing. The menu normally executes it on selection, but the text can also reach a send
 * whole — the operator typed it out and pressed Enter with the menu dismissed — and a
 * `/compact` that silently became a message to the agent would be the worst of both. */
export function resolveSlashSubmission(
  text: string,
  skills: readonly ComposerSkillRow[],
): { skill: string | null; command: string | null; text: string } {
  const none = { skill: null, command: null, text };
  if (!text.startsWith(SKILL_TRIGGER_CHAR)) return none;
  const word = text.slice(1).split(' ')[0] ?? '';
  if (word === '') return none;
  // Exact name only. A leading '/word' that names nothing installed is a typo, and sending
  // it verbatim shows the operator their own mistake instead of quietly attaching some
  // near-miss skill to an ordinary turn.
  const known = resolveSlashWord(word, skills);
  if (known === undefined) return none;
  return known.type === COMMAND_ITEM_TYPE
    ? { skill: null, command: known.id, text }
    : { skill: known.id, command: null, text };
}

import type { Unstable_TriggerItem } from '@assistant-ui/react';

// commands.ts — the '/' menu's non-skill half.
//
// A command is a verb the COMPOSER performs, not a message the agent answers, and that is
// the whole difference from a skill: a skill is written into the turn ('/pdf estrai …') and
// sent, a command runs and nothing is sent. The popover expresses both through one behavior
// primitive (TriggerPopover.Action), and this file is what lets the handler tell them apart
// without matching on names.
//
// There is exactly one command. `/new` and `/clear` used to sit here too and were removed on
// the operator's call: one duplicated the sidebar's new-chat button, the other emptied the
// box you are typing in. A menu entry has to earn the row it occupies.

/** The trigger item `type` every command carries; the skill items carry SKILL_ITEM_TYPE. */
export const COMMAND_ITEM_TYPE = 'command';

/** `/compact` — condense the earlier turns into the conversation's durable summary. */
export const COMPACT_COMMAND = 'compact';

export interface ComposerCommand {
  /** The word typed after '/', and the id the executor dispatches on. */
  readonly name: string;
  /** i18n key under `chat.skillPicker` for the row's label. */
  readonly labelKey: string;
  /** i18n key under `chat.skillPicker` for the row's second line. */
  readonly descriptionKey: string;
}

export const COMPOSER_COMMANDS: readonly ComposerCommand[] = [
  { name: COMPACT_COMMAND, labelKey: 'cmdCompact', descriptionKey: 'cmdCompactSubtitle' },
];

/** The command named by a bare `/word`, or undefined when no command has that name. */
export function findCommand(name: string): ComposerCommand | undefined {
  const needle = name.toLowerCase();
  return COMPOSER_COMMANDS.find((command) => command.name === needle);
}

/** Translate one command into a trigger item. `translate` is i18next's `t` bound to the
 * `chat.skillPicker` namespace by the caller, so this file holds no react/i18n import. */
export function commandTriggerItem(
  command: ComposerCommand,
  translate: (key: string) => string,
): Unstable_TriggerItem {
  return {
    id: command.name,
    type: COMMAND_ITEM_TYPE,
    label: translate(command.labelKey),
    description: translate(command.descriptionKey),
  };
}

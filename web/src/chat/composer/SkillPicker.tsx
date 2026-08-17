import { useCallback, useMemo } from 'react';
import { ComposerPrimitive, useAui } from '@assistant-ui/react';
import type { Unstable_TriggerItem } from '@assistant-ui/react';
import { BookOpen, Scissors, Sparkles, SquareTerminal, type LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { ComposerSkillRow } from './api';
import { COMMAND_ITEM_TYPE, COMPOSER_COMMANDS, commandTriggerItem } from './commands';
import {
  SKILL_TRIGGER_CHAR,
  createSkillDirectiveFormatter,
  createSkillTriggerAdapter,
} from './skillTrigger';
import { cn } from '@/lib/utils';

// SkillPicker: the '/' menu of skills and commands, rendered by
// ComposerPrimitive.Unstable_TriggerPopover.
//
// This file is presentation ONLY. Trigger detection, the open/close lifecycle, keyboard
// navigation (arrows / Enter / Escape), the highlighted-item bookkeeping, scroll-into-view
// and the listbox/option ARIA all belong to the primitive; the popover renders a
// role="listbox" wrapper when open and nothing at all when closed. What used to live here —
// a wrap-around active index, an Escape latch, a key→action map and a hand-written
// aria-activedescendant — was the library's half of the job, and it is where '/pdf' came to
// pin docx.
//
// Server strings render as auto-escaped React text nodes: zero HTML-injection surface.

const SKILL_ICON_FOR_TYPE: Partial<Record<string, LucideIcon>> = {
  instruction: BookOpen,
  executable: SquareTerminal,
};

function iconFor(item: Unstable_TriggerItem): LucideIcon {
  if (item.type === COMMAND_ITEM_TYPE) return Scissors;
  const skillType = item.metadata?.skillType;
  return (typeof skillType === 'string' ? SKILL_ICON_FOR_TYPE[skillType] : undefined) ?? Sparkles;
}

export interface SkillPickerProps {
  readonly skills: readonly ComposerSkillRow[];
  readonly disabled?: boolean;
  /** Runs the chosen command by name. Absent ⇒ the composer was mounted without a thread to
   * act on, so no command is offered at all rather than offered and inert. */
  readonly onCommand?: ((name: string) => void) | undefined;
}

export function SkillPicker({ skills, disabled = false, onCommand }: SkillPickerProps) {
  const { t } = useTranslation();
  const aui = useAui();
  const commands = useMemo(
    () =>
      onCommand === undefined
        ? []
        : COMPOSER_COMMANDS.map((command) =>
            commandTriggerItem(command, (key) => t(`chat.skillPicker.${key}`)),
          ),
    [onCommand, t],
  );
  const adapter = useMemo(() => createSkillTriggerAdapter(skills, commands), [skills, commands]);
  const formatter = useMemo(() => createSkillDirectiveFormatter(skills), [skills]);

  // The behavior primitive inserts the serialized directive and THEN calls this
  // (triggerSelectionResource: kind 'action' → insertDirective, onExecute). For a skill that
  // is the whole point — '/pdf ' is the turn the operator sends. For a command the text is
  // not a message, so the executor takes it back out: `removeOnExecute` would strip it for
  // both, and the flag belongs to the behavior rather than to the item.
  const execute = useCallback(
    (item: Unstable_TriggerItem) => {
      if (item.type !== COMMAND_ITEM_TYPE || onCommand === undefined) return;
      aui.composer.setText('');
      onCommand(item.id);
    },
    [aui, onCommand],
  );

  // D-09 degrade: with nothing to list the trigger is not registered at all, so '/' stays a
  // literal character instead of opening an empty menu.
  if (skills.length === 0 && commands.length === 0) return null;

  return (
    <ComposerPrimitive.Unstable_TriggerPopover
      char={SKILL_TRIGGER_CHAR}
      adapter={adapter}
      aria-label={t('chat.skillPicker.skillsHeader')}
      className="absolute bottom-full left-0 z-50 mb-2 w-full min-w-[16rem] max-w-md overflow-hidden rounded-xl border border-border bg-surface-2 shadow-[var(--shadow-popover)]"
    >
      <ComposerPrimitive.Unstable_TriggerPopover.Action formatter={formatter} onExecute={execute} />
      <ComposerPrimitive.Unstable_TriggerPopoverItems className="max-h-72 overflow-y-auto p-1.5">
        {(items) =>
          items.map((item, index) => {
            const Icon = iconFor(item);
            return (
              <ComposerPrimitive.Unstable_TriggerPopoverItem
                key={item.id}
                item={item}
                index={index}
                disabled={disabled}
                className={cn(
                  'flex w-full items-start gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors',
                  'hover:bg-surface-3/60 data-[highlighted]:bg-surface-3',
                )}
              >
                <Icon
                  data-icon
                  aria-hidden="true"
                  className={cn(
                    'mt-0.5 size-4 shrink-0',
                    item.type === COMMAND_ITEM_TYPE ? 'text-accent-text' : 'text-text-faint',
                  )}
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-text">
                    {item.label}
                  </span>
                  {item.description !== undefined && item.description.length > 0 ? (
                    <span className="block truncate text-[11px] text-text-muted">
                      {item.description}
                    </span>
                  ) : null}
                </span>
              </ComposerPrimitive.Unstable_TriggerPopoverItem>
            );
          })
        }
      </ComposerPrimitive.Unstable_TriggerPopoverItems>
    </ComposerPrimitive.Unstable_TriggerPopover>
  );
}

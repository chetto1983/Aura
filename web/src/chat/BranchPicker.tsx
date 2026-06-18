import { useTranslation } from 'react-i18next';
import { AuiIf, BranchPickerPrimitive } from '@assistant-ui/react';

// BranchPicker (D-09 / CHAT-05): the sibling-branch navigator the operator uses after an
// edit (a user turn) or a regenerate (an assistant turn) forks the conversation tree. It
// binds to the external-store runtime's branch navigation (BranchPickerPrimitive
// .Previous/.Next/.Number/.Count over getBranches/switchToBranch) — the runtime models the
// tree from the DB-walked message list ExternalStoreChat feeds it via setMessages, so this
// component is pure presentation (RESEARCH "Don't Hand-Roll a branch state machine").
//
// It renders ONLY when the message has >1 branch (MessagePrimitive.If hasBranches), so a
// non-branched turn shows nothing. The ACTIVE branch indicator (the {current}/{count}
// readout) uses the accent (reserved item 6, UI-SPEC §Color). Controls carry aria-labels;
// the readout is announced via aria-live="polite"; the chevron glyphs are aria-hidden.

export function BranchPicker() {
  const { t } = useTranslation();
  return (
    <AuiIf condition={({ message }) => message.branchCount > 1}>
      <BranchPickerPrimitive.Root
        className="flex items-center gap-1 text-[0.75rem] text-text-muted"
        hideWhenSingleBranch
      >
        <BranchPickerPrimitive.Previous
          aria-label={t('chat.branch.previous')}
          className="inline-flex h-6 w-6 items-center justify-center rounded-[var(--radius-sm)] outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-40"
        >
          <span aria-hidden="true">‹</span>
        </BranchPickerPrimitive.Previous>

        {/* The active-branch readout: accent-tinted (reserved item 6), announced politely
            so a screen reader hears the branch change without moving focus. */}
        <span aria-live="polite" className="px-1 font-mono tabular-nums text-accent">
          <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
        </span>

        <BranchPickerPrimitive.Next
          aria-label={t('chat.branch.next')}
          className="inline-flex h-6 w-6 items-center justify-center rounded-[var(--radius-sm)] outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-40"
        >
          <span aria-hidden="true">›</span>
        </BranchPickerPrimitive.Next>
      </BranchPickerPrimitive.Root>
    </AuiIf>
  );
}

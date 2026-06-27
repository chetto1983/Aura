import { useEffect, useRef, useState } from 'react';
import { Bell } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useApprovals } from './useApprovals';
import { Button } from '@/components/ui/button';

// ApprovalBadge (APRV-01 / D-04) is the persistent header chrome: an accent count
// pill (UI-SPEC accent reserved item 4 — the one thing demanding attention) showing
// how many ask_user interrupts are pending ACROSS ALL threads. The count is polled
// (useApprovals) so a background/scheduled interrupt the operator is not viewing
// still raises the badge.
//
// A11y (UI-SPEC §Interaction): the button carries the count in an aria-label; a
// SEPARATE aria-live="polite" region announces a CHANGE in the count — politely, so
// it never interrupts the operator mid-task (NOT assertive). When nothing is
// pending the pill is not rendered (never a stale "0").

export interface ApprovalBadgeProps {
  /** Toggle the cross-thread list popover (owned by the parent AppShell). */
  readonly onToggle: () => void;
  /** Whether the list popover is currently open (drives aria-expanded). */
  readonly expanded: boolean;
}

export function ApprovalBadge({ onToggle, expanded }: ApprovalBadgeProps) {
  const { t } = useTranslation();
  const { data } = useApprovals();
  const count = data?.length ?? 0;

  // Announce a count CHANGE politely. The live region text is set only when the
  // count actually changes, so the SR speaks the new total without interrupting.
  const [announce, setAnnounce] = useState('');
  const prevCount = useRef(count);
  useEffect(() => {
    if (count !== prevCount.current) {
      prevCount.current = count;
      setAnnounce(count > 0 ? t('approval.badge.aria', { count }) : t('approval.badge.cleared'));
    }
  }, [count, t]);

  return (
    <>
      {/* Polite live region: announces the new count on change, never interrupts. */}
      <span aria-live="polite" className="sr-only">
        {announce}
      </span>
      {count > 0 ? (
        <Button
          type="button"
          size="sm"
          onClick={onToggle}
          aria-expanded={expanded}
          aria-label={t('approval.badge.aria', { count })}
          className="rounded-full px-3 text-[0.75rem]"
        >
          <Bell data-icon aria-hidden="true" className="size-3.5" />
          <span className="font-mono tabular-nums">{count}</span>
        </Button>
      ) : null}
    </>
  );
}

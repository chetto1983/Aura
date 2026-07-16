import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { InlineApprovalCard } from './InlineApprovalCard';
import type { Approval } from './useApprovals';
import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';

interface Announcement {
  readonly id: number;
  readonly text: string;
}

export interface ThreadApprovalCardsProps {
  /** Already-filtered active-thread rows in deterministic backend order. */
  readonly approvals: readonly Approval[];
  readonly isStreaming?: boolean;
  readonly onResolutionStarted?: (attempt: ApprovalResolutionAttempt) => void;
  readonly onResolutionFailed?: (attempt: ApprovalResolutionAttempt) => void | Promise<void>;
  readonly onResolved?: (resolution: ApprovalResolution) => void | Promise<void>;
}

export function ThreadApprovalCards({
  approvals,
  isStreaming,
  onResolutionStarted,
  onResolutionFailed,
  onResolved,
}: ThreadApprovalCardsProps) {
  const { t } = useTranslation();
  const [announcement, setAnnouncement] = useState<Announcement>({ id: 0, text: '' });

  function handleResolved(resolution: ApprovalResolution) {
    const key =
      resolution.action === 'accept'
        ? 'approval.card.answered'
        : resolution.action === 'decline'
          ? 'approval.card.declined'
          : 'approval.card.cancelled';
    setAnnouncement((current) => ({ id: current.id + 1, text: t(key) }));
    void onResolved?.(resolution);
  }

  return (
    <div
      data-testid="thread-approvals"
      className={approvals.length > 0 ? 'flex flex-col gap-2 px-3 pb-2 sm:px-4' : undefined}
    >
      {approvals.map((approval) => (
        <InlineApprovalCard
          key={approval.token}
          approval={approval}
          {...(isStreaming !== undefined ? { isStreaming } : {})}
          {...(onResolutionStarted !== undefined ? { onResolutionStarted } : {})}
          {...(onResolutionFailed !== undefined ? { onResolutionFailed } : {})}
          onResolved={handleResolved}
        />
      ))}
      <p
        key={announcement.id}
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="sr-only"
      >
        {announcement.text}
      </p>
    </div>
  );
}

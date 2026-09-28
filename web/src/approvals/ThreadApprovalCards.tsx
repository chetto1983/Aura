import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ElicitationCard } from '../questions/ElicitationCard';
import type { ElicitationItem } from '../questions/useThreadElicitations';
import { InlineApprovalCard } from './InlineApprovalCard';
import { MessageDraftCard } from './MessageDraftCard';
import type { Approval } from './useApprovals';
import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';
import type { ThreadMessageDrafts } from './useThreadMessageDrafts';

interface Announcement {
  readonly id: number;
  readonly text: string;
}

export interface ThreadApprovalCardsProps {
  /** Already-filtered active-thread rows in deterministic backend order. */
  readonly approvals: readonly Approval[];
  readonly messageDrafts?: ThreadMessageDrafts;
  /** A mounted MCP server's forms for this thread, in arrival order. useThreadElicitations
   *  drops them with their run, so each one here is still worth showing. */
  readonly elicitations?: readonly ElicitationItem[];
  readonly isStreaming?: boolean;
  readonly onResolutionStarted?: (attempt: ApprovalResolutionAttempt) => void;
  readonly onResolutionFailed?: (attempt: ApprovalResolutionAttempt) => void | Promise<void>;
  readonly onResolved?: (resolution: ApprovalResolution) => void | Promise<void>;
}

export function ThreadApprovalCards({
  approvals,
  messageDrafts,
  elicitations = [],
  isStreaming,
  onResolutionStarted,
  onResolutionFailed,
  onResolved,
}: ThreadApprovalCardsProps) {
  const { t } = useTranslation();
  const [announcement, setAnnouncement] = useState<Announcement>({ id: 0, text: '' });
  const streaming = isStreaming !== undefined ? { isStreaming } : {};

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
      className={
        approvals.length + elicitations.length + (messageDrafts?.drafts.length ?? 0) > 0
          ? 'flex flex-col gap-2 px-3 pb-2 sm:px-4'
          : undefined
      }
    >
      {messageDrafts?.drafts.map((draft) => (
        <MessageDraftCard
          key={draft.id}
          draft={draft}
          busy={messageDrafts.isResolving}
          onResolve={messageDrafts.resolve}
        />
      ))}
      {approvals.map((approval) => (
        <InlineApprovalCard
          key={approval.token}
          approval={approval}
          {...streaming}
          {...(onResolutionStarted !== undefined ? { onResolutionStarted } : {})}
          {...(onResolutionFailed !== undefined ? { onResolutionFailed } : {})}
          onResolved={handleResolved}
        />
      ))}
      {elicitations.map((item) => (
        <ElicitationCard key={item.question.id} item={item} {...streaming} />
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

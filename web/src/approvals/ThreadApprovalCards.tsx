import { InlineApprovalCard } from './InlineApprovalCard';
import { useApprovals } from './useApprovals';

// ThreadApprovalCards (D-03) renders the inline approval card(s) for the ACTIVE
// thread inside the chat lane region. It filters the cross-thread pending poll
// (useApprovals) down to the open conversation, so an ask_user interrupt that fired
// in THIS thread surfaces as an in-thread card answered in place. Interrupts in
// OTHER threads stay in the cross-thread badge/list until the operator jumps there
// (the badge keeps them visible — D-04).
//
// On a successful accept/decline the run is re-driven (continue-after-resume) via
// onResolved so the stream continues over the rehydrated history.

export interface ThreadApprovalCardsProps {
  /** The open conversation id; only its pending interrupts render here. */
  readonly conversationId: string;
  /** True while this thread's run is streaming — gates the Cancel-run confirm. */
  readonly isStreaming?: boolean;
  /** Re-drive the run after a successful accept/decline. */
  readonly onResolved?: (conversationId: string) => void;
}

export function ThreadApprovalCards({
  conversationId,
  isStreaming,
  onResolved,
}: ThreadApprovalCardsProps) {
  const { data } = useApprovals();
  if (conversationId.length === 0) return null;
  const forThread = (data ?? []).filter((a) => a.conversation_id === conversationId);
  if (forThread.length === 0) return null;

  return (
    <div className="flex flex-col gap-2 px-3 pb-2 sm:px-4">
      {forThread.map((approval) => (
        <InlineApprovalCard
          key={approval.token}
          approval={approval}
          {...(isStreaming !== undefined ? { isStreaming } : {})}
          {...(onResolved !== undefined ? { onResolved } : {})}
        />
      ))}
    </div>
  );
}

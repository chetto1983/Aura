import { createContext, useContext, type ReactNode } from 'react';
import { ElicitationCard } from './ElicitationCard';
import type { ElicitationItem } from './useThreadElicitations';

// A settled MCP form is drawn under the row of the call it arrived in, where the thread tells
// what happened, and not above the composer, where the receipts of a turn stacked under the
// model's answer, away from their calls (lab VM, 2026-11-27).

const ReceiptsContext = createContext<readonly ElicitationItem[]>([]);

export function ToolCallReceiptsProvider({
  receipts,
  children,
}: {
  readonly receipts: readonly ElicitationItem[];
  readonly children: ReactNode;
}) {
  return <ReceiptsContext.Provider value={receipts}>{children}</ReceiptsContext.Provider>;
}

/** The receipts of these calls, in arrival order; nothing when they have none. */
export function ToolCallReceipts({ toolCallIds }: { readonly toolCallIds: readonly string[] }) {
  const receipts = useContext(ReceiptsContext).filter(
    (item) =>
      item.question.tool_call_id !== undefined && toolCallIds.includes(item.question.tool_call_id),
  );
  if (receipts.length === 0) return null;
  return (
    <div className="mt-2 flex flex-col gap-2">
      {receipts.map((item) => (
        <ElicitationCard key={item.question.id} item={item} />
      ))}
    </div>
  );
}

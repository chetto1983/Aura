import type { ThreadMessageLike } from '@assistant-ui/react';
import type { BudgetLimit } from './sseAdapter_usage';

// messageBudgetLimit reads the budget trip sseAdapter_parts.toThreadMessage put on the
// assistant message's metadata (amendment #188). Kept out of BudgetLimitNotice.tsx so
// that file exports only the component (react-refresh boundary).
export function messageBudgetLimit(message: ThreadMessageLike): BudgetLimit | undefined {
  const custom = message.metadata?.custom as { budgetLimit?: unknown } | undefined;
  const limit = custom?.budgetLimit;
  if (limit === null || typeof limit !== 'object') return undefined;
  const { reason, stepsConsumed } = limit as { reason?: unknown; stepsConsumed?: unknown };
  if (typeof reason !== 'string' || reason === '') return undefined;
  return { reason, stepsConsumed: typeof stepsConsumed === 'number' ? stepsConsumed : 0 };
}

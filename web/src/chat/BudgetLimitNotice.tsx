import { useTranslation } from 'react-i18next';
import type { BudgetLimit } from './sseAdapter_usage';

// BudgetLimitNotice — the visible half of a budget trip (amendment #188). The agent
// has always ended a cut-off turn with limit_hit/steps_consumed on the wire; the
// cockpit rendered the synthesized answer and nothing else, so an operator could not
// tell a turn that finished from one that was stopped at 25 steps. Read off the
// assistant message's metadata (budgetLimit.ts) — a status line under the answer,
// never a message part the runtime could branch from.

export function BudgetLimitNotice({ limit }: { readonly limit: BudgetLimit | undefined }) {
  const { t } = useTranslation();
  if (limit === undefined) return null;
  const key =
    limit.reason === 'max_steps'
      ? 'chat.budget.maxSteps'
      : limit.reason === 'wallclock'
        ? 'chat.budget.wallclock'
        : 'chat.budget.other';
  return (
    <p
      role="status"
      data-budget-limit={limit.reason}
      className="rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-[0.8rem] leading-snug text-warning"
    >
      {t(key, { steps: limit.stepsConsumed, reason: limit.reason })}
    </p>
  );
}

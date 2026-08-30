import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { messageBudgetLimit } from '../budgetLimit';
import { BudgetLimitNotice } from '../BudgetLimitNotice';

describe('BudgetLimitNotice', () => {
  it('reads the trip off message metadata and rejects malformed shapes', () => {
    expect(
      messageBudgetLimit({
        id: 'm',
        role: 'assistant',
        content: [],
        metadata: { custom: { budgetLimit: { reason: 'max_steps', stepsConsumed: 25 } } },
      }),
    ).toEqual({ reason: 'max_steps', stepsConsumed: 25 });
    expect(messageBudgetLimit({ id: 'm', role: 'assistant', content: [] })).toBe(undefined);
    expect(
      messageBudgetLimit({
        id: 'm',
        role: 'assistant',
        content: [],
        metadata: { custom: { budgetLimit: { reason: '' } } },
      }),
    ).toBe(undefined);
  });

  it('names the cap that cut the turn and what to do next; renders nothing otherwise', () => {
    const { rerender } = render(
      <BudgetLimitNotice limit={{ reason: 'max_steps', stepsConsumed: 25 }} />,
    );
    const status = screen.getByRole('status');
    expect(status.getAttribute('data-budget-limit')).toBe('max_steps');
    expect(status.textContent).toContain('step limit (25 steps)');
    expect(status.textContent).toContain('Send "continue"');

    rerender(<BudgetLimitNotice limit={{ reason: 'wallclock', stepsConsumed: 9 }} />);
    expect(screen.getByRole('status').textContent).toContain('time limit after 9 steps');

    rerender(<BudgetLimitNotice limit={{ reason: 'dedup', stepsConsumed: 3 }} />);
    expect(screen.getByRole('status').textContent).toContain('(dedup) after 3 steps');

    rerender(<BudgetLimitNotice limit={undefined} />);
    expect(screen.queryByRole('status')).toBeNull();
  });
});

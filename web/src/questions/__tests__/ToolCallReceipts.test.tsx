import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import type { ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import { ToolCallReceipts, ToolCallReceiptsProvider } from '../ToolCallReceipts';
import type { ElicitationItem, ElicitationOutcome } from '../useThreadElicitations';

function receipt(id: string, toolCallId: string, outcome: ElicitationOutcome): ElicitationItem {
  const question: ElicitationQuestion = {
    run_id: 'run-1',
    id,
    server: 'everything',
    tool: 'trigger-elicitation-request',
    tool_call_id: toolCallId,
    message: `form ${id}`,
    fields: [],
    deadline: '2026-11-27T13:20:00Z',
  };
  return { question, outcome };
}

const RECEIPTS = [
  receipt('a', 'call-1', 'declined'),
  receipt('b', 'call-2', 'expired'),
  receipt('c', 'call-1', 'accepted'),
];

function renderUnder(toolCallIds: readonly string[]) {
  return render(
    <ToolCallReceiptsProvider receipts={RECEIPTS}>
      <ToolCallReceipts toolCallIds={toolCallIds} />
    </ToolCallReceiptsProvider>,
  );
}

describe('ToolCallReceipts', () => {
  // Measured on the lab VM 2026-11-27: the receipts stacked under the answer, away from their calls.
  it("draws a call's receipts under it, in arrival order, and no other call's", () => {
    renderUnder(['call-1']);
    expect(screen.getByText('Declined.')).toBeTruthy();
    expect(screen.queryByText('Expired: cancelled automatically.')).toBeNull();
    const descriptions = screen.getAllByText(/^form /).map((node) => node.textContent);
    expect(descriptions).toEqual(['form a', 'form c']);
  });

  it("draws a group's receipts under the group", () => {
    renderUnder(['call-1', 'call-2']);
    expect(screen.getAllByText(/^form /)).toHaveLength(3);
  });

  it('draws nothing for a call with no receipt', () => {
    const { container } = renderUnder(['call-9']);
    expect(container.innerHTML).toBe('');
  });
});

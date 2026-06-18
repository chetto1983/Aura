import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SwarmReportTable } from '../SwarmReportTable';
import type { DisplayChildReport, DisplayPayload } from '../types';

// SwarmReportTable (SWARM-01 / D-08): one row per ChildReport, row-expand to
// summary/error (+question/options for needs-input), status from the enum ONLY, no
// mailbox / inter-agent-chat theater.

function payload(swarm: readonly DisplayChildReport[]): DisplayPayload {
  return { type: 'swarm_report', tool_call_id: 'call-1', swarm };
}

const reports: readonly DisplayChildReport[] = [
  { goal_index: 0, child_id: 'w1', status: 'ok', summary: 'Found three sources.' },
  {
    goal_index: 1,
    child_id: 'w2',
    status: 'failed',
    summary: 'Could not complete.',
    error: 'connect tcp 10.0.0.1:443: blocked',
  },
  {
    goal_index: 2,
    child_id: 'w3',
    status: 'needs_user_input',
    summary: 'Awaiting a choice.',
    question: 'Which city?',
    options: ['Rome', 'Milan'],
  },
];

describe('SwarmReportTable (SWARM-01 / D-08)', () => {
  it('renders one row per ChildReport with goal index, worker id, status, summary', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    expect(screen.getByText('w1')).toBeTruthy();
    expect(screen.getByText('w2')).toBeTruthy();
    expect(screen.getByText('w3')).toBeTruthy();
    expect(screen.getByText('Found three sources.')).toBeTruthy();
    // Status labels come from the enum.
    expect(screen.getByText('OK')).toBeTruthy();
    expect(screen.getByText('Failed')).toBeTruthy();
    expect(screen.getByText('Needs input')).toBeTruthy();
  });

  it('expands a row in place to show summary + error', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    // The failed worker's error is only shown once its row is expanded.
    expect(screen.queryByText('connect tcp 10.0.0.1:443: blocked')).toBeNull();
    fireEvent.click(screen.getByText('w2'));
    expect(screen.getByText('connect tcp 10.0.0.1:443: blocked')).toBeTruthy();
    expect(screen.getByText('Error')).toBeTruthy();
  });

  it('shows question + options for a needs_user_input child on expand', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    fireEvent.click(screen.getByText('w3'));
    expect(screen.getByText('Which city?')).toBeTruthy();
    expect(screen.getByText('Rome')).toBeTruthy();
    expect(screen.getByText('Milan')).toBeTruthy();
  });

  it('toggles a row closed when clicked again', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    const row = screen.getByText('w2');
    fireEvent.click(row);
    expect(screen.getByText('connect tcp 10.0.0.1:443: blocked')).toBeTruthy();
    fireEvent.click(row);
    expect(screen.queryByText('connect tcp 10.0.0.1:443: blocked')).toBeNull();
  });

  it('conveys status by dot + text (a labelled status, not color alone)', () => {
    const { container } = render(<SwarmReportTable payload={payload(reports)} />);
    // The dots are present (color), each paired with a text label.
    expect(container.querySelector('.bg-success')).not.toBeNull();
    expect(container.querySelector('.bg-danger')).not.toBeNull();
    expect(container.querySelector('.bg-warning')).not.toBeNull();
  });

  it('uses the enum status word, NOT the free-form error text, in the row status cell', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    const failedRow = screen.getByText('w2').closest('button') as HTMLElement;
    // The collapsed row's status cell reads "Failed" (enum), never the error string.
    expect(within(failedRow).getByText('Failed')).toBeTruthy();
    expect(within(failedRow).queryByText(/connect tcp/)).toBeNull();
  });

  it('falls to a safe "Unknown" label for an out-of-enum status', () => {
    render(
      <SwarmReportTable
        payload={payload([
          { goal_index: 0, child_id: 'wX', status: 'weird' as 'ok', summary: 's' },
        ])}
      />,
    );
    expect(screen.getByText('Unknown')).toBeTruthy();
  });

  it('renders NO mailbox / inter-agent-chat affordance (negative, SWARM-01)', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    expect(screen.queryByText(/mailbox/i)).toBeNull();
    expect(screen.queryByText(/inbox/i)).toBeNull();
    expect(screen.queryByText(/message.*agent/i)).toBeNull();
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it('shows the empty state when there are no workers', () => {
    render(<SwarmReportTable payload={payload([])} />);
    expect(screen.getByText('No workers')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });
});

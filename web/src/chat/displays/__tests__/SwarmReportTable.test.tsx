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
  it('renders the four column headers (# / Worker / Status / Summary)', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers).toEqual(['#', 'Worker', 'Status', 'Summary']);
  });

  it('renders one row per ChildReport with goal index, worker id, status, summary', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    expect(screen.getByText('w1')).toBeTruthy();
    expect(screen.getByText('w2')).toBeTruthy();
    expect(screen.getByText('w3')).toBeTruthy();
    expect(screen.getByText('Found three sources.')).toBeTruthy();
    // The goal index renders in the first cell of each row.
    expect(screen.getByText('0')).toBeTruthy();
    expect(screen.getByText('1')).toBeTruthy();
    expect(screen.getByText('2')).toBeTruthy();
    // Status labels come from the enum.
    expect(screen.getByText('OK')).toBeTruthy();
    expect(screen.getByText('Failed')).toBeTruthy();
    expect(screen.getByText('Needs input')).toBeTruthy();
  });

  it('shows the summary text in the expand region for an OK child, in the muted tone', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    // Before expand: the summary value renders once (collapsed cell).
    expect(screen.getAllByText('Found three sources.')).toHaveLength(1);
    fireEvent.click(screen.getByText('w1'));
    // After expand: it renders twice (collapsed cell + the Summary field value).
    const both = screen.getAllByText('Found three sources.');
    expect(both).toHaveLength(2);
    // The expand-field value uses the muted tone (Field default tone, not danger).
    const fieldValue = both.find((el) => el.tagName.toLowerCase() === 'dd');
    expect(fieldValue?.className).toContain('text-text-muted');
  });

  it('expands a row in place to show summary + error in the danger tone', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    // The failed worker's error is only shown once its row is expanded.
    expect(screen.queryByText('connect tcp 10.0.0.1:443: blocked')).toBeNull();
    fireEvent.click(screen.getByText('w2'));
    const errorValue = screen.getByText('connect tcp 10.0.0.1:443: blocked');
    expect(errorValue).toBeTruthy();
    expect(screen.getByText('Error')).toBeTruthy();
    // The error field value carries the danger tone (Field tone="danger").
    expect(errorValue.className).toContain('text-danger');
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

  it('does not render the Error/Question/Options fields when they are empty/absent', () => {
    render(
      <SwarmReportTable
        payload={payload([
          { goal_index: 0, child_id: 'w1', status: 'ok', summary: 'done', error: '' },
        ])}
      />,
    );
    fireEvent.click(screen.getByText('w1'));
    // The expand region carries the Summary field but no Error/Question/Options for
    // an OK child with an empty error (the column header "Summary" is a separate <th>).
    expect(screen.getByRole('definition')).toBeTruthy();
    expect(screen.queryByText('Error')).toBeNull();
    expect(screen.queryByText('Question')).toBeNull();
    expect(screen.queryByText('Options')).toBeNull();
  });

  it('falls to "No summary reported." when a child has no summary', () => {
    render(
      <SwarmReportTable payload={payload([{ goal_index: 0, child_id: 'w1', status: 'ok' }])} />,
    );
    // The collapsed row shows the placeholder; expanding shows it in the Summary field.
    expect(screen.getAllByText('No summary reported.').length).toBeGreaterThan(0);
  });

  it('the row toggle exposes aria-expanded reflecting open state', () => {
    render(<SwarmReportTable payload={payload(reports)} />);
    const toggle = screen.getByText('w1').closest('button') as HTMLElement;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
  });

  it('shows the empty state when there are no workers', () => {
    render(<SwarmReportTable payload={payload([])} />);
    expect(screen.getByText('No workers')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });
});

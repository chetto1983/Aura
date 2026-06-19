import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ChartDisplay } from '../ChartDisplay';
import type { DisplayPayload } from '../types';

// ChartDisplay (D-02): zero-dependency bars + an accessible <table> fallback. The
// suite pins the no-charting-library contract by reading the component source.

function payload(
  x_labels: readonly string[],
  y_values: readonly number[],
  x_axis_label?: string,
): DisplayPayload {
  return {
    type: 'chart',
    tool_call_id: 'call-1',
    chart: { x_labels, y_values, ...(x_axis_label !== undefined ? { x_axis_label } : {}) },
  };
}

const sales = payload(['Q1', 'Q2', 'Q3'], [10, 25, 5], 'Quarter');

describe('ChartDisplay (D-02)', () => {
  it('renders an accessible <table> fallback carrying every label and value', () => {
    render(<ChartDisplay payload={sales} />);
    const table = screen.getByRole('table');
    // The data behind the bars is in the table (the authoritative path).
    expect(within(table).getByText('Q1')).toBeTruthy();
    expect(within(table).getByText('Q2')).toBeTruthy();
    expect(within(table).getByText('Q3')).toBeTruthy();
    expect(within(table).getByText('25')).toBeTruthy();
    // The axis label heads the category column (the column header, not the caption).
    expect(within(table).getByRole('columnheader', { name: 'Quarter' })).toBeTruthy();
  });

  it('renders width-scaled bars (the tallest value reaches 100%)', () => {
    const { container } = render(<ChartDisplay payload={sales} />);
    const bars = container.querySelectorAll('[style*="width"]');
    expect(bars.length).toBe(3);
    const widths = Array.from(bars).map((b) => (b as HTMLElement).style.width);
    // The max value (25) is the full bar; the smallest (5) is 20%.
    expect(widths).toContain('100%');
    expect(widths).toContain('20%');
  });

  it('marks the decorative bar region aria-hidden so the table is the read path', () => {
    const { container } = render(<ChartDisplay payload={sales} />);
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull();
  });

  it('shows the "No data" empty state when there are no values', () => {
    render(<ChartDisplay payload={payload([], [])} />);
    expect(screen.getByText('No data')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('zips a label/value length mismatch to the shorter list (no crash)', () => {
    render(<ChartDisplay payload={payload(['A', 'B', 'C'], [1])} />);
    const table = screen.getByRole('table');
    expect(within(table).getByText('A')).toBeTruthy();
    expect(within(table).queryByText('B')).toBeNull();
  });

  it('imports NO charting library (zero-dep contract, D-02)', () => {
    const source = readFileSync(join(process.cwd(), 'src/chat/displays/ChartDisplay.tsx'), 'utf8');
    expect(source).not.toMatch(/from ['"]recharts['"]/);
    expect(source).not.toMatch(/from ['"]uplot['"]/);
    expect(source).not.toMatch(/from ['"]chart\.js['"]/);
    expect(source).not.toMatch(/from ['"]d3['"]/);
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { TableDisplay } from '../TableDisplay';
import type { DisplayPayload } from '../types';

// TableDisplay (DISP-03 / D-14): sort, filter, copy, CSV export, and in-card row
// pagination (default 3/page), all client-side, with the omit-when-valid filter and
// announced sort state.

function payload(
  columns: readonly string[],
  rows: readonly (readonly string[])[],
): DisplayPayload {
  return { type: 'table', tool_call_id: 'call-1', table: { columns, rows } };
}

const fruits = payload(
  ['Name', 'Qty'],
  [
    ['Cherry', '3'],
    ['Apple', '10'],
    ['Banana', '1'],
    ['Date', '7'],
  ],
);

describe('TableDisplay (DISP-03 / D-14)', () => {
  it('renders a native table with the columns and the first 3 rows by default', () => {
    render(<TableDisplay payload={fruits} />);
    expect(screen.getByRole('table')).toBeTruthy();
    expect(screen.getByText('Cherry')).toBeTruthy();
    expect(screen.getByText('Apple')).toBeTruthy();
    expect(screen.getByText('Banana')).toBeTruthy();
    // Row 4 is on page 2 (default 3/page).
    expect(screen.queryByText('Date')).toBeNull();
    expect(screen.getByText('1–3 of 4')).toBeTruthy();
  });

  it('paginates rows at 3/page; next advances the window', () => {
    render(<TableDisplay payload={fruits} />);
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('Date')).toBeTruthy();
    expect(screen.getByText('4–4 of 4')).toBeTruthy();
    expect(screen.queryByText('Cherry')).toBeNull();
  });

  it('sorts ascending then descending when a column header is clicked, and announces state', () => {
    render(<TableDisplay payload={fruits} />);
    const sortName = screen.getByRole('button', { name: 'Sort by Name' });

    // First click → ascending. Page-1 window shows the three smallest names.
    fireEvent.click(sortName);
    const headerAsc = sortName.closest('th');
    expect(headerAsc?.getAttribute('aria-sort')).toBe('ascending');
    // Apple, Banana, Cherry are the first 3 ascending; Date is on page 2.
    expect(screen.getByText('Apple')).toBeTruthy();
    expect(screen.queryByText('Date')).toBeNull();
    expect(within(headerAsc as HTMLElement).getByText('ascending')).toBeTruthy();

    // Second click → descending.
    fireEvent.click(sortName);
    expect(sortName.closest('th')?.getAttribute('aria-sort')).toBe('descending');
  });

  it('sorts the numeric column numerically, not lexically', () => {
    render(<TableDisplay payload={fruits} />);
    fireEvent.click(screen.getByRole('button', { name: 'Sort by Qty' }));
    // Numeric ascending: 1, 3, 7 on page 1 (NOT lexical "1","10","3").
    expect(screen.getByText('Banana')).toBeTruthy(); // qty 1
    expect(screen.getByText('Cherry')).toBeTruthy(); // qty 3
    expect(screen.getByText('Date')).toBeTruthy(); // qty 7
    expect(screen.queryByText('Apple')).toBeNull(); // qty 10 → page 2
  });

  it('filters rows by the query and shows "No matches" with the query when nothing hits', () => {
    render(<TableDisplay payload={fruits} />);
    const filter = screen.getByPlaceholderText('Filter rows');

    fireEvent.change(filter, { target: { value: 'app' } });
    expect(screen.getByText('Apple')).toBeTruthy();
    expect(screen.queryByText('Cherry')).toBeNull();
    expect(screen.getByText('1–1 of 1')).toBeTruthy();

    fireEvent.change(filter, { target: { value: 'zzz' } });
    expect(screen.getByText('No matches')).toBeTruthy();
    expect(screen.getByText('No rows match "zzz". Clear the filter to see all rows.')).toBeTruthy();
  });

  it('uses aria-invalid omit-when-valid on the filter input (attr absent when valid)', () => {
    render(<TableDisplay payload={fruits} />);
    const filter = screen.getByPlaceholderText('Filter rows');
    expect(filter.hasAttribute('aria-invalid')).toBe(false);
  });

  it('shows the "No rows" empty state when the payload has no rows', () => {
    render(<TableDisplay payload={payload(['A', 'B'], [])} />);
    expect(screen.getByText('No rows')).toBeTruthy();
    expect(screen.getByText('This result has no tabular data.')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  describe('clipboard / CSV', () => {
    let writeText: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      writeText = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText },
      });
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('copies the table as TSV and flips to the "Copied" confirmation', async () => {
      vi.useFakeTimers();
      render(<TableDisplay payload={fruits} />);
      const copyBtn = screen.getByRole('button', { name: 'Copy table' });
      fireEvent.click(copyBtn);
      expect(writeText).toHaveBeenCalledTimes(1);
      const text = writeText.mock.calls[0]?.[0] as string;
      expect(text).toContain('Name\tQty');
      expect(text).toContain('Cherry\t3');
      // The transient confirmation label replaces the idle label.
      await vi.waitFor(() => {
        expect(screen.getByRole('button', { name: 'Copy table' }).textContent).toBe('Copied');
      });
    });

    it('exports CSV with header + comma-separated rows', () => {
      render(<TableDisplay payload={fruits} />);
      fireEvent.click(screen.getByRole('button', { name: 'Export table as CSV' }));
      expect(writeText).toHaveBeenCalledTimes(1);
      const csv = writeText.mock.calls[0]?.[0] as string;
      expect(csv).toContain('Name,Qty');
      expect(csv).toContain('Apple,10');
    });

    it('CSV-quotes a field that contains a comma', () => {
      render(<TableDisplay payload={payload(['Note'], [['a, b']])} />);
      fireEvent.click(screen.getByRole('button', { name: 'Export table as CSV' }));
      const csv = writeText.mock.calls[0]?.[0] as string;
      expect(csv).toContain('"a, b"');
    });
  });
});

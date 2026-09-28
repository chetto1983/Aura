import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { DataTable } from '../data-table';

const columns = [
  { key: 'c0', label: 'File', priority: 'primary' as const },
  { key: 'c1', label: 'Count', priority: 'secondary' as const },
];

describe('DataTable', () => {
  it('renders escaped cells in a semantic table and labelled narrow cards', () => {
    render(
      <DataTable columns={columns} rows={[{ c0: '<b>safe</b>', c1: '3' }]} caption="Results" />,
    );
    const table = screen.getByRole('table', { name: 'Results' });
    expect(within(table).getByText('<b>safe</b>')).toBeTruthy();
    expect(document.querySelector('b')).toBeNull();
    const cards = document.querySelector('[data-slot="data-table-cards"]');
    expect(cards?.textContent).toContain('Count');
    expect(cards?.textContent).toContain('3');
  });

  it('cycles controlled sort ascending, descending, then unsorted', () => {
    const onSortChange = vi.fn();
    const { rerender } = render(
      <DataTable
        columns={columns}
        rows={[
          { c0: 'b', c1: '2' },
          { c0: 'a', c1: '1' },
        ]}
        sort={null}
        onSortChange={onSortChange}
      />,
    );
    fireEvent.click(
      within(screen.getByRole('table')).getByRole('button', { name: 'Sort by File' }),
    );
    expect(onSortChange).toHaveBeenCalledWith({ key: 'c0', direction: 'asc' });
    rerender(
      <DataTable
        columns={columns}
        rows={[
          { c0: 'b', c1: '2' },
          { c0: 'a', c1: '1' },
        ]}
        sort={{ key: 'c0', direction: 'asc' }}
        onSortChange={onSortChange}
      />,
    );
    fireEvent.click(
      within(screen.getByRole('table')).getByRole('button', { name: 'Sort by File' }),
    );
    expect(onSortChange).toHaveBeenLastCalledWith({ key: 'c0', direction: 'desc' });
    rerender(
      <DataTable
        columns={columns}
        rows={[
          { c0: 'b', c1: '2' },
          { c0: 'a', c1: '1' },
        ]}
        sort={{ key: 'c0', direction: 'desc' }}
        onSortChange={onSortChange}
      />,
    );
    fireEvent.click(
      within(screen.getByRole('table')).getByRole('button', { name: 'Sort by File' }),
    );
    expect(onSortChange).toHaveBeenLastCalledWith(null);
  });

  it('offers the same sorting action beside narrow cards', () => {
    const onSortChange = vi.fn();
    render(
      <DataTable
        columns={columns}
        rows={[{ c0: 'b', c1: '2' }]}
        sort={null}
        onSortChange={onSortChange}
      />,
    );
    const cards = document.querySelector('[data-slot="data-table-cards"]') as HTMLElement;
    fireEvent.click(within(cards).getByRole('button', { name: 'Sort by File' }));
    expect(onSortChange).toHaveBeenCalledWith({ key: 'c0', direction: 'asc' });
  });
});

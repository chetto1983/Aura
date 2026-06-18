import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { DisplayPagination } from '../DisplayPagination';

function items(n: number) {
  return Array.from({ length: n }, (_, i) => <div key={i}>{`item-${String(i + 1)}`}</div>);
}

describe('DisplayPagination (D-PAGINATION)', () => {
  it('shows the first 3 of 7 items and the "1–3 of 7" count by default', () => {
    render(<DisplayPagination>{items(7)}</DisplayPagination>);
    expect(screen.getByText('item-1')).toBeTruthy();
    expect(screen.getByText('item-2')).toBeTruthy();
    expect(screen.getByText('item-3')).toBeTruthy();
    // Page 2's items are not rendered.
    expect(screen.queryByText('item-4')).toBeNull();
    expect(screen.getByText('1–3 of 7')).toBeTruthy();
  });

  it('next advances the window to "4–6 of 7" and prev returns', () => {
    render(<DisplayPagination>{items(7)}</DisplayPagination>);
    const next = screen.getByRole('button', { name: 'Next page' });
    fireEvent.click(next);
    expect(screen.getByText('4–6 of 7')).toBeTruthy();
    expect(screen.getByText('item-4')).toBeTruthy();
    expect(screen.queryByText('item-1')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(screen.getByText('1–3 of 7')).toBeTruthy();
    expect(screen.getByText('item-1')).toBeTruthy();
  });

  it('the last page shows the partial window "7–7 of 7"', () => {
    render(<DisplayPagination>{items(7)}</DisplayPagination>);
    const next = screen.getByRole('button', { name: 'Next page' });
    fireEvent.click(next); // 4–6
    fireEvent.click(next); // 7–7
    expect(screen.getByText('7–7 of 7')).toBeTruthy();
    expect(screen.getByText('item-7')).toBeTruthy();
  });

  it('prev is disabled on the first page; next is disabled on the last', () => {
    render(<DisplayPagination>{items(7)}</DisplayPagination>);
    const prev = screen.getByRole('button', { name: 'Previous page' });
    const next = screen.getByRole('button', { name: 'Next page' });
    expect(prev.hasAttribute('disabled')).toBe(true);
    expect(next.hasAttribute('disabled')).toBe(false);
    fireEvent.click(next);
    fireEvent.click(next);
    expect(next.hasAttribute('disabled')).toBe(true);
  });

  it('the per-page select changes the window and resets to the first page', () => {
    render(<DisplayPagination>{items(7)}</DisplayPagination>);
    // Advance, then change per-page to 6 → resets to page 1 with a 6-item window.
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    const select = screen.getByLabelText('Per page');
    fireEvent.change(select, { target: { value: '6' } });
    expect(screen.getByText('1–6 of 7')).toBeTruthy();
    expect(screen.getByText('item-6')).toBeTruthy();
    expect(screen.queryByText('item-7')).toBeNull();
  });

  it('prev/next controls have 44px touch targets (min-h-11 min-w-11)', () => {
    render(<DisplayPagination>{items(7)}</DisplayPagination>);
    const next = screen.getByRole('button', { name: 'Next page' });
    expect(next.className).toContain('min-h-11');
    expect(next.className).toContain('min-w-11');
  });

  it('hides the prev/next controls when everything fits on one page', () => {
    render(<DisplayPagination>{items(2)}</DisplayPagination>);
    expect(screen.getByText('1–2 of 2')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Previous page' })).toBeNull();
  });

  it('renders nothing when there are no children', () => {
    const { container } = render(<DisplayPagination>{[]}</DisplayPagination>);
    expect(container.firstChild).toBeNull();
  });

  it('honors a custom itemsPerPage', () => {
    render(<DisplayPagination itemsPerPage={3}>{items(5)}</DisplayPagination>);
    expect(screen.getByText('1–3 of 5')).toBeTruthy();
  });
});

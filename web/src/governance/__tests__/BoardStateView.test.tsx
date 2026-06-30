import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { BoardStateView } from '../governanceView';

function renderState(status: Parameters<typeof BoardStateView>[0]['status']) {
  const onRetry = vi.fn();
  render(
    <BoardStateView
      status={status}
      emptyHeading="Nothing here"
      emptyBody="Add something to continue."
      onRetry={onRetry}
    >
      <div>populated content</div>
    </BoardStateView>,
  );
  return { onRetry };
}

describe('BoardStateView', () => {
  it('renders loading with skeleton blocks', () => {
    renderState('loading');
    expect(screen.getByRole('status')).toBeTruthy();
    expect(document.querySelectorAll('.skeleton-block').length).toBeGreaterThan(0);
  });

  it('renders empty with the shadcn Empty primitive', () => {
    renderState('empty');
    expect(screen.getByText('Nothing here').closest('[data-slot="empty"]')).not.toBeNull();
    expect(screen.getByText('Add something to continue.')).toBeTruthy();
  });

  it('renders retryable errors with Alert and Button primitives', () => {
    const { onRetry } = renderState('error');
    const alert = screen.getByRole('alert');
    expect(alert.getAttribute('data-slot')).toBe('alert');

    const retry = screen.getByRole('button', { name: 'Retry' });
    expect(retry.getAttribute('data-slot')).toBe('button');
    fireEvent.click(retry);
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});

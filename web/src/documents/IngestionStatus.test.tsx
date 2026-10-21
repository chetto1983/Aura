import { act, render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { IngestionStatus } from './IngestionStatus';
import i18n from '@/i18n/i18n';

it('describes active work despite ready and keeps raw component counters in a closed disclosure', async () => {
  await act(() => i18n.changeLanguage('en'));
  const { container } = render(
    <IngestionStatus
      state={{
        status: 'ready',
        observed_at: '2026-10-22T12:30:00Z',
        in_progress: 8,
        finished: 13543,
        errors: 2,
      }}
    />,
  );
  expect(screen.getByText('Indexing documents')).toBeTruthy();
  expect(screen.getByText('Reported indexing issues: 2')).toBeTruthy();
  expect(container.querySelector('details')?.open).toBe(false);
  expect(screen.queryByText(/Last observation: 2026-10-22T/)).toBeNull();
  expect(container.textContent).not.toContain('failed files');
});

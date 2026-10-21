import { act, fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { DocumentCards } from './DocumentCards';
import { PAGE } from './documentsTestServer';
import i18n from '@/i18n/i18n';

it('reloads revised thumbnails and clears a previous revision failure', async () => {
  await act(() => i18n.changeLanguage('en'));
  const first = PAGE.rows[0];
  if (!first) throw new Error('Missing document fixture');
  const row = { ...first, file_type: 'pdf' as const, raw_sha256: 'first' };
  const props = {
    names: {
      type: new Map<string, string>(),
      counterparty: new Map<string, string>(),
      tags: new Map<string, string>(),
    },
    cards: true,
    selection: {
      isSelected: () => false,
      toggle: vi.fn(),
      togglePage: vi.fn(),
      pageState: 'none' as const,
    },
    onOpen: vi.fn(),
  };
  const { rerender } = render(<DocumentCards {...props} rows={[row]} />);
  const before = screen.getByRole('img').getAttribute('src');
  fireEvent.error(screen.getByRole('img'));
  expect(screen.queryByRole('img')).toBeNull();
  expect(screen.getByText('PDF')).toBeTruthy();
  rerender(<DocumentCards {...props} rows={[{ ...row, raw_sha256: 'second' }]} />);
  expect(screen.getByRole('img').getAttribute('src')).not.toBe(before);
  expect(screen.getByRole('img').getAttribute('src')).toContain('v=second');
});

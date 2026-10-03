import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { ShareLinkRow } from './ShareLinkRow';
import { EXPIRY_TICK_MS } from './shareViewModel';

// A row stays mounted for as long as its panel is open, so its expiry has to follow the real
// clock rather than the moment it mounted.
describe('ShareLinkRow expiry clock', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('turns expired while mounted once the clock passes expires_at', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-10-03T10:00:00Z'));
    const { container } = render(
      <ShareLinkRow
        link={{
          id: 'share-1',
          tier: 'internal',
          created_at: '2026-10-01T00:00:00Z',
          updated_at: '2026-10-01T00:00:00Z',
          expires_at: '2026-10-03T10:00:30Z',
        }}
        onRevokeClick={vi.fn()}
      />,
    );
    expect(container.querySelector('[data-expired="false"]')).toBeTruthy();
    expect(screen.queryByText('Expired')).toBeNull();

    act(() => {
      vi.advanceTimersByTime(EXPIRY_TICK_MS);
    });

    expect(container.querySelector('[data-expired="true"]')).toBeTruthy();
    expect(screen.getByText('Expired')).toBeTruthy();
  });
});

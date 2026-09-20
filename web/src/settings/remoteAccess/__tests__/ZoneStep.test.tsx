import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n';
import { ZoneStep } from '../ZoneStep';

describe('ZoneStep', () => {
  it('requires an explicit account selection when Cloudflare returns several accounts', () => {
    render(
      <ZoneStep
        accounts={[
          { id: 'one', name: 'One' },
          { id: 'two', name: 'Two' },
        ]}
        generation={1}
        initialDomain="example.com"
        onSave={vi.fn(() => Promise.resolve())}
      />,
    );
    const account = screen.getByLabelText('Cloudflare account') as HTMLSelectElement;
    expect(account.value).toBe('');
    expect(
      (screen.getByRole('button', { name: 'Save and start setup' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    fireEvent.change(account, { target: { value: 'two' } });
    expect(
      (screen.getByRole('button', { name: 'Save and start setup' }) as HTMLButtonElement).disabled,
    ).toBe(false);
  });
});

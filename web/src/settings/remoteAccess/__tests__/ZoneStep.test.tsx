import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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
        onSave={vi.fn(() => Promise.resolve())}
      />,
    );
    expect(screen.getByLabelText('Cloudflare account').getAttribute('value') ?? '').toBe('');
  });
});

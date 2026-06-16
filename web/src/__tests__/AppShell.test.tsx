import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AppShell } from '../AppShell';
import i18n from '../i18n/i18n';

// The marketing-hero copy this operator console must NOT ship (ux-spec §350 / SC4).
const MARKETING_HERO_BLOCKLIST = [
  /get started for free/i,
  /sign up today/i,
  /trusted by/i,
  /supercharge/i,
  /unlock your/i,
  /the future of/i,
];

// AppShell now mounts the RuntimeHealthPanel, which polls /healthz + /readyz via
// React Query — so the shell needs a QueryClient and a stubbed fetch in tests.
function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AppShell />
    </QueryClientProvider>,
  );
}

describe('AppShell', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 })),
      ),
    );
  });

  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('renders the Aura brand mark', () => {
    renderShell();
    expect(screen.getByRole('img', { name: /aura/i })).toBeTruthy();
  });

  it('switches the cockpit shell to Italian', () => {
    renderShell();
    fireEvent.click(screen.getByRole('button', { name: 'Italiano' }));

    expect(screen.getByRole('navigation', { name: 'Principale' })).toBeTruthy();
    expect(screen.getByText('Albero')).toBeTruthy();
    expect(screen.getByText('Indagini')).toBeTruthy();
    expect(screen.getByLabelText('Area display')).toBeTruthy();
  });

  it('ships no marketing-hero copy in the primary viewport', () => {
    const { container } = renderShell();
    const text = container.textContent ?? '';
    for (const pattern of MARKETING_HERO_BLOCKLIST) {
      expect(text).not.toMatch(pattern);
    }
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
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

// AppShell now mounts the RuntimeHealthPanel (polls /healthz + /readyz) AND the
// conversation sidebar/search (GET /api/conversations) + the runtime footer, so the
// shell needs a QueryClient, a Router (useParams/useNavigate), and a stubbed fetch.
function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppShell', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        // The conversation list / rot-events read as a JSON array; health as an object.
        if (url.includes('/api/conversations')) {
          return Promise.resolve(new Response('[]', { status: 200 }));
        }
        return Promise.resolve(
          new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }),
        );
      }),
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
    // The left aside now hosts the conversation manager (replaced the placeholder
    // section labels); its heading is localised.
    expect(screen.getByText('Conversazioni')).toBeTruthy();
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

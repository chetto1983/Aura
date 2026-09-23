import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import '../i18n/i18n';
import { LoginPage } from '../routes/LoginPage';

// Sign-out stays inside this SPA, so the query cache outlives the session that filled it: a
// previous admin's `me` (fresh for 60 s) would make the next identity's cockpit render admin
// surfaces. Signing in must start from an empty cache.

const PREVIOUS_ADMIN = { identity_id: 'admin', capabilities: ['identity.create'] };

function authulaConfig() {
  return new Response(
    JSON.stringify({
      provider: 'authula',
      auth_base_path: '/auth',
      csrf_header_name: 'X-AUTHULA-CSRF-TOKEN',
      csrf_token: 'csrf-token',
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

function renderWithCachedAdmin() {
  const client = new QueryClient();
  client.setQueryData(['me'], PREVIOUS_ADMIN);
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>cockpit home</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

async function submitCredentials() {
  fireEvent.change(await screen.findByLabelText('Operator email'), {
    target: { value: 'member@example.com' },
  });
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } });
  fireEvent.submit(screen.getByRole('form', { name: 'Sign in' }));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('LoginPage session boundary', () => {
  it('drops the previous identity cache on a password sign-in', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(authulaConfig())
        .mockResolvedValueOnce(new Response('{"session":{"id":"s1"}}', { status: 200 })),
    );
    const client = renderWithCachedAdmin();

    await submitCredentials();

    await screen.findByText('cockpit home');
    expect(client.getQueryData(['me'])).toBeUndefined();
  });

  it('drops the previous identity cache on a TOTP sign-in', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(authulaConfig())
        .mockResolvedValueOnce(new Response('{"totp_redirect":true}', { status: 200 }))
        .mockResolvedValueOnce(new Response('{"session":{"id":"s1"}}', { status: 200 })),
    );
    const client = renderWithCachedAdmin();

    await submitCredentials();
    fireEvent.change(await screen.findByLabelText('Verification code'), {
      target: { value: '123456' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Sign in' }));

    await screen.findByText('cockpit home');
    expect(client.getQueryData(['me'])).toBeUndefined();
  });
});

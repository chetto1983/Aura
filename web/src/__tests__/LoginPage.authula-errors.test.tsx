import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import i18n from '../i18n/i18n';
import { LoginPage } from '../routes/LoginPage';

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <LoginPage />
    </MemoryRouter>,
  );
}

function submitForm(container: HTMLElement) {
  fireEvent.submit(within(container).getByRole('form', { name: 'Sign in' }));
}

function authulaConfigResponse() {
  return Promise.resolve(
    new Response(
      JSON.stringify({
        provider: 'authula',
        auth_base_path: '/auth',
        csrf_cookie_name: '__Host-authula_csrf_token',
        csrf_header_name: 'X-AUTHULA-CSRF-TOKEN',
        csrf_token: 'csrf-token',
        bootstrap_available: false,
      }),
      {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          'X-AUTHULA-CSRF-TOKEN': 'csrf-token',
        },
      },
    ),
  );
}

const LOGIN_ARIA_TOKEN_VALUES: Readonly<Record<string, readonly string[]>> = {
  'aria-busy': ['true', 'false'],
  'aria-hidden': ['true', 'false'],
  'aria-invalid': ['true', 'false', 'grammar', 'spelling'],
  'aria-pressed': ['true', 'false', 'mixed'],
};

function expectLoginAriaValuesToBeAxeValid(container: HTMLElement) {
  const nodes = Array.from(
    container.querySelectorAll<HTMLElement>(
      '[aria-busy], [aria-hidden], [aria-invalid], [aria-pressed], [aria-describedby]',
    ),
  );

  for (const node of nodes) {
    for (const [name, allowed] of Object.entries(LOGIN_ARIA_TOKEN_VALUES)) {
      const value = node.getAttribute(name);
      if (value !== null) {
        expect(allowed, `${name}="${value}"`).toContain(value);
      }
    }

    const describedBy = node.getAttribute('aria-describedby');
    if (describedBy !== null) {
      for (const id of describedBy.trim().split(/\s+/u)) {
        expect(document.getElementById(id), `aria-describedby="${id}"`).not.toBeNull();
      }
    }
  }
}

describe('LoginPage Authula error accessibility', () => {
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('marks Authula credential errors with axe-valid aria-invalid tokens', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(new Response('unauthorized', { status: 401 }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();

    const email = await screen.findByLabelText('Operator email');
    const password = screen.getByLabelText('Password');
    fireEvent.change(email, { target: { value: 'operator@example.com' } });
    fireEvent.change(password, { target: { value: 'bad-pass' } });
    submitForm(container);

    await screen.findByRole('alert');
    expect(email.getAttribute('aria-invalid')).toBe('true');
    expect(password.getAttribute('aria-invalid')).toBe('true');
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('marks Authula code errors with axe-valid aria-invalid tokens', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ totp_redirect: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      .mockResolvedValueOnce(new Response('unauthorized', { status: 401 }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();

    fireEvent.change(await screen.findByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    submitForm(container);

    const code = await screen.findByLabelText('Verification code');
    fireEvent.change(code, { target: { value: '000000' } });
    submitForm(container);

    await screen.findByRole('alert');
    expect(code.getAttribute('aria-invalid')).toBe('true');
    expectLoginAriaValuesToBeAxeValid(container);
  });
});

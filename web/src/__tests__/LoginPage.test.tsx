import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import i18n from '../i18n/i18n';
import { LoginPage } from '../routes/LoginPage';

function renderLogin(initialEntries: string[] = ['/login']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <LoginPage />
    </MemoryRouter>,
  );
}

function submitForm(container: HTMLElement) {
  const form = within(container).getByRole('form', { name: 'Sign in' });
  fireEvent.submit(form);
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

function requireRequestBody(opts: RequestInit): string {
  if (typeof opts.body !== 'string') {
    throw new TypeError('expected request body to be a JSON string');
  }
  return opts.body;
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

describe('LoginPage', () => {
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('renders Authula credential fields by default', () => {
    const { container } = renderLogin();
    const email = screen.getByLabelText('Operator email');
    const password = screen.getByLabelText('Password');
    expect(email.getAttribute('data-slot')).toBe('input');
    expect(password.getAttribute('data-slot')).toBe('input');
    expect(screen.getByRole('main').querySelector('[data-slot="card"]')).not.toBeNull();
    expect(email.getAttribute('autocomplete')).toBe('username');
    expect(password.getAttribute('type')).toBe('password');
    expect(password.getAttribute('autocomplete')).toBe('current-password');
    expect(email.getAttribute('aria-invalid')).toBe(null);
    expect(screen.queryByLabelText('Operator passphrase')).toBe(null);
    expect(screen.getByRole('button', { name: 'Sign in' }).getAttribute('data-slot')).toBe(
      'button',
    );
    expect(screen.getByRole('button', { name: 'Forgot password?' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create first user' })).toBeTruthy();
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('uses readable accent text for secondary login actions', () => {
    renderLogin();
    const forgotPassword = screen.getByRole('button', { name: 'Forgot password?' });
    const createUser = screen.getByRole('button', { name: 'Create first user' });

    expect(forgotPassword.className).toContain('text-accent-text');
    expect(forgotPassword.className.split(/\s+/u)).not.toContain('text-accent');
    expect(createUser.className).toContain('text-accent-text');
    expect(createUser.className.split(/\s+/u)).not.toContain('text-accent');
  });

  it('renders a decorative animated particle-network background behind the form', () => {
    const { container } = renderLogin();
    const background = screen.getByTestId('login-animated-background');
    expect(background.getAttribute('aria-hidden')).toBe('true');
    expect(background.className).toContain('pointer-events-none');
    expect(container.querySelectorAll('[data-login-particle]').length).toBeGreaterThan(6);
    expect(container.querySelectorAll('[data-login-link]').length).toBeGreaterThan(4);
  });

  it('toggles password visibility via the show/hide button', () => {
    renderLogin();
    const field = screen.getByLabelText('Password');
    expect(field.getAttribute('type')).toBe('password');

    const show = screen.getByRole('button', { name: 'Show password' });
    expect(show.getAttribute('data-slot')).toBe('button');
    expect(show.getAttribute('aria-pressed')).toBe('false');

    fireEvent.click(show);
    expect(field.getAttribute('type')).toBe('text');
    const hide = screen.getByRole('button', { name: 'Hide password' });
    expect(hide.getAttribute('aria-pressed')).toBe('true');

    fireEvent.click(hide);
    expect(field.getAttribute('type')).toBe('password');
  });

  it('switches the login copy to Italian', () => {
    renderLogin();
    fireEvent.click(screen.getByRole('button', { name: 'Italiano' }));

    expect(screen.getByLabelText('Email operatore')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Accedi' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Password dimenticata?' })).toBeTruthy();
  });

  it('renders a role=alert error and sets aria-invalid on a failed submit', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(new Response('unauthorized', { status: 401 }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();

    const email = await screen.findByLabelText('Operator email');
    const password = screen.getByLabelText('Password');
    fireEvent.change(email, { target: { value: 'operator@example.com' } });
    fireEvent.change(password, { target: { value: 'wrong-pass' } });
    submitForm(container);

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe("That email or password didn't match. Check it and try again.");
    await waitFor(() => {
      expect(email.getAttribute('aria-invalid')).toBe('true');
      expect(password.getAttribute('aria-invalid')).toBe('true');
    });
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('navigates to the cockpit on a successful login', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ session: { id: 's1' } }), { status: 200 }),
      );
    vi.stubGlobal('fetch', fetchMock);
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>cockpit home</div>} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.change(await screen.findByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Sign in' }));
    expect(await screen.findByText('cockpit home')).toBeTruthy();
  });

  it('does not fall back to /login when auth config cannot be loaded', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response('no', { status: 500 }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    submitForm(container);

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe(
      "Couldn't reach Aura. Check the server is running and try again.",
    );
    const calls = fetchMock.mock.calls as unknown as [string, RequestInit][];
    expect(calls.some(([url]) => url === '/login')).toBe(false);
  });

  it('shows the in-flight CTA and disables submit while the request is pending', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockImplementationOnce(() => new Promise<Response>(() => undefined));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();
    fireEvent.change(await screen.findByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    submitForm(container);
    const inFlight = await screen.findByRole('button', { name: 'Signing in...' });
    expect((inFlight as HTMLButtonElement).disabled).toBe(true);
  });

  it('returns focus to the email field after a failed submit (WCAG 3.3.1)', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(new Response('no', { status: 401 }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();
    const field = await screen.findByLabelText('Operator email');
    submitForm(container);
    await waitFor(() => {
      expect(document.activeElement).toBe(field);
    });
  });

  it('shows the session-expired notice when redirected with ?expired=1', () => {
    renderLogin(['/login?expired=1']);
    expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
  });

  it('surfaces the network-error copy when the fetch throws', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockRejectedValueOnce(new Error('network down'));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();
    fireEvent.change(await screen.findByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    submitForm(container);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe(
      "Couldn't reach Aura. Check the server is running and try again.",
    );
  });

  it('renders Authula email/password fields when the auth config selects Authula', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => authulaConfigResponse()),
    );

    const { container } = renderLogin();

    expect(await screen.findByLabelText('Operator email')).toBeTruthy();
    expect(screen.getByLabelText('Password')).toBeTruthy();
    expect(screen.queryByLabelText('Operator passphrase')).toBe(null);
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('opens the Telegram password reset panel from the credentials step', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => authulaConfigResponse()),
    );
    renderLogin();

    fireEvent.click(screen.getByRole('button', { name: 'Forgot password?' }));

    expect(await screen.findByText('Password reset')).toBeTruthy();
    expect(screen.getByRole('form', { name: 'Request reset code' })).toBeTruthy();
    expect(screen.queryByRole('form', { name: 'Sign in' })).toBe(null);
  });

  it('creates the first operator from the login page', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/auth/config') {
        return authulaConfigResponse();
      }
      if (url === '/api/auth/bootstrap/operator') {
        return Promise.resolve(
          new Response(JSON.stringify({ identityId: '11111111-1111-1111-1111-111111111111' }), {
            status: 201,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      return Promise.resolve(new Response('not found', { status: 404 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();

    fireEvent.click(screen.getByRole('button', { name: 'Create first user' }));
    const form = await screen.findByRole('form', { name: 'Create first user' });
    expect(screen.queryByRole('form', { name: 'Sign in' })).toBe(null);

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'first@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    fireEvent.change(screen.getByLabelText('Security question'), {
      target: { value: 'First school?' },
    });
    fireEvent.change(screen.getByLabelText('Security answer'), {
      target: { value: 'Ada' },
    });
    fireEvent.submit(form);

    expect(await screen.findByText('First user created')).toBeTruthy();
    const bootstrapCall = fetchMock.mock.calls.find(
      ([url]) => url === '/api/auth/bootstrap/operator',
    );
    if (!bootstrapCall) {
      throw new Error('missing bootstrap request');
    }
    const [, opts] = bootstrapCall as unknown as [string, RequestInit];
    expect(opts.method).toBe('POST');
    expect(opts.credentials).toBe('same-origin');
    expect(opts.headers).toMatchObject({ 'Content-Type': 'application/json' });
    expect(JSON.parse(requireRequestBody(opts))).toEqual({
      email: 'first@example.com',
      password: 'correct-horse',
      securityQuestion: 'First school?',
      securityAnswer: 'Ada',
    });
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('POSTs Authula credentials as JSON with the CSRF token and advances to TOTP', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ totp_redirect: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();

    fireEvent.change(await screen.findByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    submitForm(container);

    const codeField = await screen.findByLabelText('Verification code');
    expect((codeField as HTMLInputElement).value).toBe('');
    const [url, opts] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(url).toBe('/auth/email-password/sign-in');
    expect(opts.method).toBe('POST');
    expect(opts.credentials).toBe('same-origin');
    expect(opts.headers).toMatchObject({
      'Content-Type': 'application/json',
      'X-AUTHULA-CSRF-TOKEN': 'csrf-token',
    });
    expect(JSON.parse(requireRequestBody(opts))).toEqual({
      email: 'operator@example.com',
      password: 'correct-horse',
    });
  });

  it('POSTs the Authula TOTP code and navigates to the cockpit', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => authulaConfigResponse())
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ totp_redirect: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ session: { id: 's1' } }), { status: 200 }),
      );
    vi.stubGlobal('fetch', fetchMock);

    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>cockpit home</div>} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.change(await screen.findByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct-horse' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Sign in' }));

    fireEvent.change(await screen.findByLabelText('Verification code'), {
      target: { value: '123456' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Sign in' }));

    expect(await screen.findByText('cockpit home')).toBeTruthy();
    const [url, opts] = fetchMock.mock.calls[2] as unknown as [string, RequestInit];
    expect(url).toBe('/auth/totp/verify');
    expect(opts.method).toBe('POST');
    expect(opts.headers).toMatchObject({ 'X-AUTHULA-CSRF-TOKEN': 'csrf-token' });
    expect(JSON.parse(requireRequestBody(opts))).toEqual({ code: '123456', trust_device: false });
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

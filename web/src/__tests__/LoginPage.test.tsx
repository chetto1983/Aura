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

  it('renders a labelled passphrase field with autocomplete=current-password', () => {
    const { container } = renderLogin();
    const field = screen.getByLabelText('Operator passphrase');
    expect(field).toBeTruthy();
    expect(field.getAttribute('type')).toBe('password');
    expect(field.getAttribute('autocomplete')).toBe('current-password');
    // A pristine field must NOT emit aria-invalid (omit-when-valid, not aria-invalid="false").
    expect(field.getAttribute('aria-invalid')).toBe(null);
    // The CTA keeps the name "Sign in" (frontend-design: an action keeps its name).
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy();
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('renders a decorative animated particle-network background behind the form', () => {
    const { container } = renderLogin();
    const background = screen.getByTestId('login-animated-background');
    expect(background.getAttribute('aria-hidden')).toBe('true');
    expect(background.className).toContain('pointer-events-none');
    expect(container.querySelectorAll('[data-login-particle]').length).toBeGreaterThan(6);
    expect(container.querySelectorAll('[data-login-link]').length).toBeGreaterThan(4);
  });

  it('toggles passphrase visibility via the show/hide button', () => {
    renderLogin();
    const field = screen.getByLabelText('Operator passphrase');
    expect(field.getAttribute('type')).toBe('password');

    // Pristine: the button offers to SHOW the passphrase (aria-pressed reflects state).
    const show = screen.getByRole('button', { name: 'Show passphrase' });
    expect(show.getAttribute('aria-pressed')).toBe('false');

    fireEvent.click(show);
    expect(field.getAttribute('type')).toBe('text');
    const hide = screen.getByRole('button', { name: 'Hide passphrase' });
    expect(hide.getAttribute('aria-pressed')).toBe('true');

    fireEvent.click(hide);
    expect(field.getAttribute('type')).toBe('password');
  });

  it('switches the login copy to Italian', () => {
    renderLogin();
    fireEvent.click(screen.getByRole('button', { name: 'Italiano' }));

    expect(screen.getByLabelText('Frase segreta operatore')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Accedi' })).toBeTruthy();
    expect(screen.getByText('Necessaria quando Aura è esposta oltre il loopback.')).toBeTruthy();
  });

  it('renders a role=alert error and sets aria-invalid on a failed submit', async () => {
    // The server returns a generic 401 on a wrong/unconfigured passphrase.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('unauthorized', { status: 401 }))),
    );
    const { container } = renderLogin();

    const field = screen.getByLabelText('Operator passphrase');
    fireEvent.change(field, { target: { value: 'wrong-pass' } });
    submitForm(container);

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe("That passphrase didn't match. Check it and try again.");
    await waitFor(() => {
      expect(field.getAttribute('aria-invalid')).toBe('true');
    });
    expectLoginAriaValuesToBeAxeValid(container);
  });

  it('navigates to the cockpit on a successful login', async () => {
    // A 2xx from /login means the server set the cookie and redirected; the SPA navigates to "/".
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 200 }))),
    );
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>cockpit home</div>} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.submit(screen.getByRole('form', { name: 'Sign in' }));
    expect(await screen.findByText('cockpit home')).toBeTruthy();
  });

  it('POSTs the passphrase to /login same-origin as form-encoded', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('unauthorized', { status: 401 })));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderLogin();
    fireEvent.change(screen.getByLabelText('Operator passphrase'), {
      target: { value: 'hunter2' },
    });
    submitForm(container);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    const calls = fetchMock.mock.calls as unknown as [string, RequestInit][];
    const loginCall = calls.find(([url]) => url === '/login');
    expect(loginCall).toBeTruthy();
    if (!loginCall) {
      throw new Error('missing /login fetch call');
    }
    const [url, opts] = loginCall;
    expect(url).toBe('/login');
    expect(opts.method).toBe('POST');
    expect(opts.credentials).toBe('same-origin');
    expect(opts.headers).toMatchObject({ 'Content-Type': 'application/x-www-form-urlencoded' });
    expect(opts.body).toBeInstanceOf(URLSearchParams);
    expect((opts.body as URLSearchParams).get('passphrase')).toBe('hunter2');
  });

  it('shows the in-flight CTA and disables submit while the request is pending', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => undefined)), // never resolves → stays submitting
    );
    const { container } = renderLogin();
    submitForm(container);
    const inFlight = await screen.findByRole('button', { name: 'Signing in...' });
    expect((inFlight as HTMLButtonElement).disabled).toBe(true);
  });

  it('returns focus to the passphrase field after a failed submit (WCAG 3.3.1)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('no', { status: 401 }))),
    );
    const { container } = renderLogin();
    const field = screen.getByLabelText('Operator passphrase');
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
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('network down'))),
    );
    const { container } = renderLogin();
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

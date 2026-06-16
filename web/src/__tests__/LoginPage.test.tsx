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

describe('LoginPage', () => {
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('renders a labelled passphrase field with autocomplete=current-password', () => {
    renderLogin();
    const field = screen.getByLabelText('Operator passphrase');
    expect(field).toBeTruthy();
    expect(field.getAttribute('type')).toBe('password');
    expect(field.getAttribute('autocomplete')).toBe('current-password');
    // A pristine field must NOT emit aria-invalid (omit-when-valid, not aria-invalid="false").
    expect(field.getAttribute('aria-invalid')).toBe(null);
    // The CTA keeps the name "Sign in" (frontend-design: an action keeps its name).
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy();
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
});

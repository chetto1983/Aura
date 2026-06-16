import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LoginPage } from '../routes/LoginPage';
import { LOGIN_COPY } from '../routes/loginCopy';

function renderLogin(initialEntries: string[] = ['/login']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <LoginPage />
    </MemoryRouter>,
  );
}

function submitForm(container: HTMLElement) {
  const form = within(container).getByRole('form', { name: LOGIN_COPY.cta });
  fireEvent.submit(form);
}

describe('LoginPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders a labelled passphrase field with autocomplete=current-password', () => {
    renderLogin();
    const field = screen.getByLabelText(LOGIN_COPY.fieldLabel);
    expect(field).toBeTruthy();
    expect(field.getAttribute('type')).toBe('password');
    expect(field.getAttribute('autocomplete')).toBe('current-password');
    // The CTA keeps the name "Sign in" (frontend-design: an action keeps its name).
    expect(screen.getByRole('button', { name: LOGIN_COPY.cta })).toBeTruthy();
  });

  it('renders a role=alert error and sets aria-invalid on a failed submit', async () => {
    // The server returns a generic 401 on a wrong/unconfigured passphrase.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('unauthorized', { status: 401 }))),
    );
    const { container } = renderLogin();

    const field = screen.getByLabelText(LOGIN_COPY.fieldLabel);
    fireEvent.change(field, { target: { value: 'wrong-pass' } });
    submitForm(container);

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe(LOGIN_COPY.errorWrongPassphrase);
    await waitFor(() => {
      expect(field.getAttribute('aria-invalid')).toBe('true');
    });
  });

  it('shows the session-expired notice when redirected with ?expired=1', () => {
    renderLogin(['/login?expired=1']);
    expect(screen.getByText(LOGIN_COPY.sessionExpired)).toBeTruthy();
  });

  it('surfaces the network-error copy when the fetch throws', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('network down'))),
    );
    const { container } = renderLogin();
    submitForm(container);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe(LOGIN_COPY.errorNetwork);
  });
});

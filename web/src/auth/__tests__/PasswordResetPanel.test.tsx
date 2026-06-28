import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import i18n from '../../i18n/i18n';
import { PasswordResetPanel } from '../PasswordResetPanel';

function renderPanel() {
  const onCancel = vi.fn();
  const view = render(<PasswordResetPanel onCancel={onCancel} />);
  return { onCancel, ...view };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function requireRequestBody(opts: RequestInit): string {
  if (typeof opts.body !== 'string') {
    throw new TypeError('expected request body to be a JSON string');
  }
  return opts.body;
}

describe('PasswordResetPanel', () => {
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('starts with an email field and Telegram delivery copy', () => {
    const { container } = renderPanel();

    const email = screen.getByLabelText('Operator email');
    expect(email.getAttribute('autocomplete')).toBe('username');
    expect(screen.getByText('Password reset')).toBeTruthy();
    expect(screen.getByText('Aura sends reset codes through Telegram.')).toBeTruthy();
    expect(screen.getByRole('form', { name: 'Request reset code' })).toBeTruthy();
    expect(container.querySelector('[aria-live="polite"]')).not.toBeNull();
  });

  it('requests a Telegram reset code without exposing account existence', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ status: 'ok' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Request reset code' }));

    expect(await screen.findByLabelText('Telegram code')).toBeTruthy();
    expect(
      screen.getByText('If that account has recovery enabled, check Telegram now.'),
    ).toBeTruthy();

    const [url, opts] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/auth/password-reset/start');
    expect(opts.method).toBe('POST');
    expect(opts.credentials).toBe('same-origin');
    expect(opts.headers).toMatchObject({
      Accept: 'application/json',
      'Content-Type': 'application/json',
    });
    expect(JSON.parse(requireRequestBody(opts))).toEqual({ email: 'operator@example.com' });
  });

  it('verifies the Telegram code plus security answer before asking for a new password', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Request reset code' }));

    fireEvent.change(await screen.findByLabelText('Telegram code'), {
      target: { value: '123456' },
    });
    fireEvent.change(screen.getByLabelText('Security answer'), {
      target: { value: 'blue bicycle' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Verify recovery code' }));

    expect(await screen.findByLabelText('New password')).toBeTruthy();
    const [url, opts] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(url).toBe('/api/auth/password-reset/verify');
    expect(JSON.parse(requireRequestBody(opts))).toEqual({
      email: 'operator@example.com',
      code: '123456',
      answer: 'blue bicycle',
    });
  });

  it('blocks completion when the confirmation password differs', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Request reset code' }));
    fireEvent.change(await screen.findByLabelText('Telegram code'), {
      target: { value: '123456' },
    });
    fireEvent.change(screen.getByLabelText('Security answer'), {
      target: { value: 'blue bicycle' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Verify recovery code' }));

    fireEvent.change(await screen.findByLabelText('New password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'different-password' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Set new password' }));

    await screen.findByRole('alert');
    expect(screen.getByText('The passwords do not match.')).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('completes the reset using the verified reset token', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Request reset code' }));
    fireEvent.change(await screen.findByLabelText('Telegram code'), {
      target: { value: '123456' },
    });
    fireEvent.change(screen.getByLabelText('Security answer'), {
      target: { value: 'blue bicycle' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Verify recovery code' }));
    fireEvent.change(await screen.findByLabelText('New password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Set new password' }));

    expect(await screen.findByText('Password updated')).toBeTruthy();
    const [url, opts] = fetchMock.mock.calls[2] as unknown as [string, RequestInit];
    expect(url).toBe('/api/auth/password-reset/complete');
    expect(JSON.parse(requireRequestBody(opts))).toEqual({
      resetToken: 'reset-token-1',
      password: 'correct-horse-battery',
    });
  });

  it('keeps the operator on the current step when the API rejects a request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'nope' }, 500));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'operator@example.com' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Request reset code' }));

    await screen.findByRole('alert');
    expect(
      screen.getByText("Couldn't reset the password. Check the details and try again."),
    ).toBeTruthy();
    expect(screen.getByLabelText('Operator email')).toBeTruthy();
  });

  it('returns to sign in when the secondary action is used', async () => {
    const { onCancel } = renderPanel();

    fireEvent.click(screen.getByRole('button', { name: 'Back to sign in' }));

    await waitFor(() => {
      expect(onCancel).toHaveBeenCalledTimes(1);
    });
  });
});

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

// Walks start → code → answer, leaving the panel on the answer step with the question revealed.
async function advanceToAnswerStep() {
  fireEvent.change(screen.getByLabelText('Operator email'), {
    target: { value: 'operator@example.com' },
  });
  fireEvent.submit(screen.getByRole('form', { name: 'Request reset code' }));

  fireEvent.change(await screen.findByLabelText('Telegram code'), {
    target: { value: '123456' },
  });
  fireEvent.submit(screen.getByRole('form', { name: 'Enter recovery code' }));

  await screen.findByLabelText('Security answer');
}

// Walks the full flow up to the set-new-password step.
async function advanceToCompleteStep() {
  await advanceToAnswerStep();
  fireEvent.change(screen.getByLabelText('Security answer'), {
    target: { value: 'blue bicycle' },
  });
  fireEvent.submit(screen.getByRole('form', { name: 'Answer your security question' }));
  await screen.findByLabelText('New password');
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

  it('reveals the security question only after the Telegram code is accepted', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ question: 'Favorite bike?' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    await advanceToAnswerStep();

    expect(screen.getByText('Favorite bike?')).toBeTruthy();
    const [url, opts] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(url).toBe('/api/auth/password-reset/question');
    expect(JSON.parse(requireRequestBody(opts))).toEqual({
      email: 'operator@example.com',
      code: '123456',
    });
  });

  it('verifies the security answer before asking for a new password', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ question: 'Favorite bike?' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    await advanceToAnswerStep();
    fireEvent.change(screen.getByLabelText('Security answer'), {
      target: { value: 'blue bicycle' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Answer your security question' }));

    expect(await screen.findByLabelText('New password')).toBeTruthy();
    const [url, opts] = fetchMock.mock.calls[2] as unknown as [string, RequestInit];
    expect(url).toBe('/api/auth/password-reset/verify');
    expect(JSON.parse(requireRequestBody(opts))).toEqual({
      email: 'operator@example.com',
      code: '123456',
      answer: 'blue bicycle',
    });
  });

  it('reveals and hides recovery answer and new-password fields on request', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ question: 'Favorite bike?' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    await advanceToAnswerStep();
    const answer = screen.getByLabelText('Security answer');
    expect(answer.getAttribute('type')).toBe('password');
    fireEvent.click(screen.getByRole('button', { name: 'Show Security answer' }));
    expect(answer.getAttribute('type')).toBe('text');

    fireEvent.change(answer, { target: { value: 'blue bicycle' } });
    fireEvent.submit(screen.getByRole('form', { name: 'Answer your security question' }));

    const newPassword = await screen.findByLabelText('New password');
    const confirmPassword = screen.getByLabelText('Confirm new password');
    expect(newPassword.getAttribute('type')).toBe('password');
    expect(confirmPassword.getAttribute('type')).toBe('password');

    fireEvent.click(screen.getByRole('button', { name: 'Show New password' }));
    fireEvent.click(screen.getByRole('button', { name: 'Show Confirm new password' }));

    expect(newPassword.getAttribute('type')).toBe('text');
    expect(confirmPassword.getAttribute('type')).toBe('text');
  });

  it('blocks completion when the confirmation password differs', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ question: 'Favorite bike?' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    await advanceToCompleteStep();
    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'different-password' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Set new password' }));

    await screen.findByRole('alert');
    expect(screen.getByText('The passwords do not match.')).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('surfaces a specific message when the new password matches the current one', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ question: 'Favorite bike?' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }))
      .mockResolvedValueOnce(jsonResponse({ error: 'same' }, 409));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    await advanceToCompleteStep();
    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'same-as-current' },
    });
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'same-as-current' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Set new password' }));

    await screen.findByRole('alert');
    expect(screen.getByText('Choose a password different from your current one.')).toBeTruthy();
    // The reset token stays valid: the panel remains on the set-password step for a retry.
    expect(screen.getByLabelText('New password')).toBeTruthy();
  });

  it('completes the reset using the verified reset token', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ question: 'Favorite bike?' }))
      .mockResolvedValueOnce(jsonResponse({ resetToken: 'reset-token-1' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel();

    await advanceToCompleteStep();
    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.submit(screen.getByRole('form', { name: 'Set new password' }));

    expect(await screen.findByText('Password updated')).toBeTruthy();
    const [url, opts] = fetchMock.mock.calls[3] as unknown as [string, RequestInit];
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

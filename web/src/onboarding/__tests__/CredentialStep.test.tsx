import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { CredentialStep } from '../CredentialStep';

// CredentialStep test (ONBD-01a / D-05 / T-28-06-01). Proves the email + password fields are
// controlled, the 2FA password hint renders, and — critically — the password is WRITE-ONLY: it is
// a masked type="password" field and its plaintext value is never rendered as visible DOM text.

const PASSWORD = 'My-Plaintext-PW-4242';

function Harness() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [securityQuestion, setSecurityQuestion] = useState('');
  const [securityAnswer, setSecurityAnswer] = useState('');
  return (
    <CredentialStep
      email={email}
      password={password}
      confirmPassword={confirmPassword}
      securityQuestion={securityQuestion}
      securityAnswer={securityAnswer}
      onEmailChange={setEmail}
      onPasswordChange={setPassword}
      onConfirmPasswordChange={setConfirmPassword}
      onSecurityQuestionChange={setSecurityQuestion}
      onSecurityAnswerChange={setSecurityAnswer}
    />
  );
}

describe('CredentialStep (D-05)', () => {
  it('renders the email + initial-password fields with the first-login 2FA hint', () => {
    render(<Harness />);
    expect(screen.getByLabelText('Operator email')).toBeTruthy();
    expect(screen.getByLabelText('Initial password')).toBeTruthy();
    expect(screen.getByLabelText('Confirm initial password')).toBeTruthy();
    expect(screen.getByLabelText('Security question')).toBeTruthy();
    const answer = screen.getByLabelText('Security answer');
    expect(answer.getAttribute('type')).toBe('password');
    expect(
      screen.getByText(
        'The new user sets up two-factor sign-in on first login. The password is never shown again.',
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        'Used with Telegram to reset this password. The answer is never shown again.',
      ),
    ).toBeTruthy();
  });

  it('keeps password and security answer WRITE-ONLY: masked inputs, never visible text', () => {
    const { container } = render(<Harness />);

    const password = screen.getByLabelText('Initial password');
    const confirmPassword = screen.getByLabelText('Confirm initial password');
    const answer = screen.getByLabelText('Security answer');
    // It is a masked password input (never a text input that would reveal the value).
    expect(password.getAttribute('type')).toBe('password');
    expect(confirmPassword.getAttribute('type')).toBe('password');
    expect(answer.getAttribute('type')).toBe('password');

    fireEvent.change(password, { target: { value: PASSWORD } });
    fireEvent.change(confirmPassword, { target: { value: PASSWORD } });
    fireEvent.change(answer, { target: { value: 'blue bicycle' } });
    // The value is held by the controlled input (to send on provision)...
    expect((password as HTMLInputElement).value).toBe(PASSWORD);
    expect((confirmPassword as HTMLInputElement).value).toBe(PASSWORD);
    expect((answer as HTMLInputElement).value).toBe('blue bicycle');
    // ...but it is NEVER rendered as visible DOM text anywhere (no echo / readback).
    expect(container.textContent).not.toContain(PASSWORD);
    expect(container.textContent).not.toContain('blue bicycle');
  });

  it('reveals and hides the initial password and security answer on explicit request', () => {
    render(<Harness />);

    const password = screen.getByLabelText('Initial password');
    const confirmPassword = screen.getByLabelText('Confirm initial password');
    const answer = screen.getByLabelText('Security answer');
    expect(password.getAttribute('type')).toBe('password');
    expect(confirmPassword.getAttribute('type')).toBe('password');
    expect(answer.getAttribute('type')).toBe('password');

    fireEvent.click(screen.getByRole('button', { name: 'Show Initial password' }));
    fireEvent.click(screen.getByRole('button', { name: 'Show Confirm initial password' }));
    fireEvent.click(screen.getByRole('button', { name: 'Show Security answer' }));

    expect(password.getAttribute('type')).toBe('text');
    expect(confirmPassword.getAttribute('type')).toBe('text');
    expect(answer.getAttribute('type')).toBe('text');

    fireEvent.click(screen.getByRole('button', { name: 'Hide Initial password' }));
    fireEvent.click(screen.getByRole('button', { name: 'Hide Confirm initial password' }));
    fireEvent.click(screen.getByRole('button', { name: 'Hide Security answer' }));

    expect(password.getAttribute('type')).toBe('password');
    expect(confirmPassword.getAttribute('type')).toBe('password');
    expect(answer.getAttribute('type')).toBe('password');
  });

  it('shows an inline mismatch alert when the password confirmation differs', () => {
    render(<Harness />);

    fireEvent.change(screen.getByLabelText('Initial password'), { target: { value: PASSWORD } });
    const confirmPassword = screen.getByLabelText('Confirm initial password');
    fireEvent.change(confirmPassword, { target: { value: 'not-the-same' } });

    expect(screen.getByRole('alert').textContent).toContain('The passwords do not match.');
    expect(confirmPassword.getAttribute('aria-invalid')).toBe('true');
  });

  it('drives the controlled email value', () => {
    render(<Harness />);
    const email = screen.getByLabelText('Operator email');
    fireEvent.change(email, { target: { value: 'new@example.com' } });
    expect((email as HTMLInputElement).value).toBe('new@example.com');
  });

  it('drives the controlled security question and answer values', () => {
    render(<Harness />);
    const question = screen.getByLabelText('Security question');
    const answer = screen.getByLabelText('Security answer');
    fireEvent.change(question, { target: { value: 'First school?' } });
    fireEvent.change(answer, { target: { value: 'blue' } });
    expect((question as HTMLInputElement).value).toBe('First school?');
    expect((answer as HTMLInputElement).value).toBe('blue');
  });
});

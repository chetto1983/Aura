import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SeedProfileForm } from '../SeedProfileForm';
import type { OnboardingSeed } from '../onboardingApi';

// SeedProfileForm test (Amendment #95) — the typed seed form that replaced the LLM interview.
// It proves the controlled contract (every keystroke reaches onChange with the OTHER fields
// preserved), the byte-identical value invariant (no trim, no case-fold, non-ASCII survives), the
// per-field cap error, and the omit-when-valid aria-invalid pattern.

function Harness({ initial = {} }: { readonly initial?: OnboardingSeed }) {
  const [seed, setSeed] = useState<OnboardingSeed>(initial);
  return (
    <>
      <SeedProfileForm value={seed} onChange={setSeed} />
      <output data-testid="state">{JSON.stringify(seed)}</output>
    </>
  );
}

function state() {
  return JSON.parse(screen.getByTestId('state').textContent ?? '{}') as OnboardingSeed;
}

describe('SeedProfileForm', () => {
  it('renders exactly the six seed fields, all optional, each with its own label', () => {
    render(<SeedProfileForm value={{}} onChange={vi.fn()} />);
    for (const label of [
      'Name',
      'Language',
      'Where you are',
      'Time zone',
      'What you do',
      'Organisation',
    ]) {
      expect(screen.getByLabelText(label)).toBeTruthy();
    }
    // No interview remnants.
    expect(screen.queryByLabelText('Your answer')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('reports the typed name BYTE-IDENTICAL — no trim, no normalisation', () => {
    render(<Harness />);
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: '  José-María 李雷  ' } });
    expect(state().name).toBe('  José-María 李雷  ');
  });

  it('preserves the other fields when one changes (a spread, not a replace)', () => {
    render(<Harness initial={{ name: 'Davide', role: 'founder' }} />);
    fireEvent.change(screen.getByLabelText('Organisation'), { target: { value: 'PmSync' } });
    expect(state()).toEqual({ name: 'Davide', role: 'founder', company: 'PmSync' });
  });

  it('fills every field independently', () => {
    render(<Harness />);
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Davide' } });
    fireEvent.change(screen.getByLabelText('Language'), { target: { value: 'it' } });
    fireEvent.change(screen.getByLabelText('Where you are'), { target: { value: 'Caraglio' } });
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'Europe/Rome' } });
    fireEvent.change(screen.getByLabelText('What you do'), { target: { value: 'founder' } });
    fireEvent.change(screen.getByLabelText('Organisation'), { target: { value: 'PmSync' } });
    expect(state()).toEqual({
      name: 'Davide',
      lang: 'it',
      location: 'Caraglio',
      timezone: 'Europe/Rome',
      role: 'founder',
      company: 'PmSync',
    });
  });

  it('offers the two cockpit languages plus an explicit "not specified" default', () => {
    render(<SeedProfileForm value={{}} onChange={vi.fn()} />);
    const select = screen.getByLabelText<HTMLSelectElement>('Language');
    expect([...select.options].map((o) => o.value)).toEqual(['', 'it', 'en']);
    expect(select.value).toBe('');
  });

  it('offers IANA zones as a datalist so the timezone stays free-text with autocomplete', () => {
    const { container } = render(<SeedProfileForm value={{}} onChange={vi.fn()} />);
    const list = container.querySelector('datalist');
    expect(list).not.toBeNull();
    expect((list?.querySelectorAll('option').length ?? 0) > 100).toBe(true);
    expect(screen.getByLabelText('Time zone').getAttribute('list')).toBe(list?.id);
  });

  it('OMITS aria-invalid while a field is valid (never renders aria-invalid="false")', () => {
    const { container } = render(<SeedProfileForm value={{ name: 'Davide' }} onChange={vi.fn()} />);
    expect(screen.getByLabelText('Name').hasAttribute('aria-invalid')).toBe(false);
    expect(container.querySelector('[aria-invalid="false"]')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('flags a field past its own server cap with aria-invalid + a described error', () => {
    render(<SeedProfileForm value={{ name: 'x'.repeat(257) }} onChange={vi.fn()} />);
    const input = screen.getByLabelText('Name');
    expect(input.getAttribute('aria-invalid')).toBe('true');
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('too long');
    expect(input.getAttribute('aria-describedby')).toBe(alert.id);
  });

  it('applies each cap to its OWN field — 33 chars is over for lang but fine for name', () => {
    render(
      <SeedProfileForm value={{ name: 'x'.repeat(33), lang: 'x'.repeat(33) }} onChange={vi.fn()} />,
    );
    expect(screen.getByLabelText('Name').hasAttribute('aria-invalid')).toBe(false);
    expect(screen.getByLabelText('Language').getAttribute('aria-invalid')).toBe('true');
  });

  // Without a name the mapper keys every fact off the placeholder subject "operator" and writes no
  // PERSON entity for them to resolve against — the orphan shape Amendment #95 exists to end.
  it('flags a missing name once ANY other field is filled, and only then', () => {
    const { rerender } = render(<SeedProfileForm value={{}} onChange={vi.fn()} />);
    expect(screen.getByLabelText('Name').hasAttribute('aria-invalid')).toBe(false);
    expect(screen.queryByRole('alert')).toBeNull();

    rerender(<SeedProfileForm value={{ role: 'founder' }} onChange={vi.fn()} />);
    const input = screen.getByLabelText('Name');
    expect(input.getAttribute('aria-invalid')).toBe('true');
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('Add your name');
    expect(input.getAttribute('aria-describedby')).toBe(alert.id);

    rerender(<SeedProfileForm value={{ name: 'Davide', role: 'founder' }} onChange={vi.fn()} />);
    expect(screen.getByLabelText('Name').hasAttribute('aria-invalid')).toBe(false);
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('points aria-describedby at the help text while the field is valid', () => {
    render(<SeedProfileForm value={{}} onChange={vi.fn()} />);
    const input = screen.getByLabelText('Name');
    const helpId = input.getAttribute('aria-describedby') ?? '';
    expect(document.getElementById(helpId)?.textContent).toContain('How Aura should address you');
  });
});

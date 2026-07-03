import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../i18n';
import { LanguageSwitcher } from '../LanguageSwitcher';

describe('LanguageSwitcher', () => {
  it('renders compact language toggles with a 32px desktop hit target', () => {
    render(<LanguageSwitcher />);

    const english = screen.getByRole('button', { name: 'English' });
    const italian = screen.getByRole('button', { name: 'Italiano' });
    expect(english.className).toContain('h-[32px]');
    expect(english.className).toContain('min-h-[32px]');
    expect(italian.className).toContain('min-w-[32px]');
  });
});

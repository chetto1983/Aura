import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { THEME_STORAGE_KEY } from '../applyTheme';
import { ThemeSwitcher } from '../ThemeSwitcher';

describe('ThemeSwitcher', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.setAttribute('data-theme', 'dark');
  });

  afterEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });

  it('switches between light and dark themes with persisted state', () => {
    render(<ThemeSwitcher />);

    expect(screen.getByRole('button', { name: 'Light theme' }).getAttribute('data-slot')).toBe(
      'button',
    );
    expect(screen.getByRole('button', { name: 'Light theme' }).className).toContain('h-[32px]');
    expect(screen.getByRole('button', { name: 'Dark theme' }).className).toContain('min-h-[32px]');
    fireEvent.click(screen.getByRole('button', { name: 'Light theme' }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');

    fireEvent.click(screen.getByRole('button', { name: 'Dark theme' }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });
});

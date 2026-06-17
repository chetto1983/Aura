import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  applyTheme,
  DENSITY_STORAGE_KEY,
  getDensity,
  getTheme,
  setTheme,
  setDensity,
  THEME_STORAGE_KEY,
} from '../theme/applyTheme';

describe('applyTheme', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.removeAttribute('data-density');
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it('defaults to the dark theme + operator density when storage is empty', () => {
    expect(getTheme()).toBe('dark');
    expect(getDensity()).toBe('operator');
  });

  it('uses the namespaced aura.* storage keys', () => {
    expect(THEME_STORAGE_KEY).toBe('aura.theme');
    expect(DENSITY_STORAGE_KEY).toBe('aura.density');
  });

  it('setDensity writes under the density key only (not the theme key)', () => {
    setDensity('review');
    expect(localStorage.getItem(DENSITY_STORAGE_KEY)).toBe('review');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });

  it('reads a valid stored density and ignores an invalid one', () => {
    localStorage.setItem(DENSITY_STORAGE_KEY, 'review');
    expect(getDensity()).toBe('review');
    localStorage.setItem(DENSITY_STORAGE_KEY, 'bogus');
    expect(getDensity()).toBe('operator');
  });

  it('ignores an invalid stored theme', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'solarized');
    expect(getTheme()).toBe('dark');
  });

  it('honours valid stored light and dark themes', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'light');
    expect(getTheme()).toBe('light');
    localStorage.setItem(THEME_STORAGE_KEY, 'dark');
    expect(getTheme()).toBe('dark');
  });

  it('applyTheme writes data-theme + data-density on <html>', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'light');
    localStorage.setItem(DENSITY_STORAGE_KEY, 'compact');
    applyTheme();
    const root = document.documentElement;
    expect(root.getAttribute('data-theme')).toBe('light');
    expect(root.getAttribute('data-density')).toBe('compact');
  });

  it('setTheme persists to storage and reflects on <html>', () => {
    setTheme('light');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });

  it('setDensity persists to storage and reflects on <html>', () => {
    setDensity('review');
    expect(localStorage.getItem(DENSITY_STORAGE_KEY)).toBe('review');
    expect(document.documentElement.getAttribute('data-density')).toBe('review');
  });

  it('survives storage throwing (private mode): defaults on read, DOM-only on write', () => {
    const throwing = () => {
      throw new Error('storage disabled');
    };
    // The code under test only calls getItem/setItem; a minimal throwing stub
    // exercises the try/catch fallbacks without an over-broad Storage shim.
    vi.stubGlobal('localStorage', { getItem: throwing, setItem: throwing });
    // read() swallows the throw and falls back to the defaults.
    expect(getDensity()).toBe('operator');
    expect(getTheme()).toBe('dark');
    // setDensity swallows the throw but still sets the in-DOM attribute.
    setDensity('compact');
    expect(document.documentElement.getAttribute('data-density')).toBe('compact');
    setTheme('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });
});

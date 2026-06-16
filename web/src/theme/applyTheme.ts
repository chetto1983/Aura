import { DEFAULT_DENSITY, isDensity, type Density } from './density';

export const THEME_STORAGE_KEY = 'aura.theme';
export const DENSITY_STORAGE_KEY = 'aura.density';
export const DEFAULT_THEME = 'dark';

export type Theme = 'dark';

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function getDensity(): Density {
  const stored = read(DENSITY_STORAGE_KEY);
  return isDensity(stored) ? stored : DEFAULT_DENSITY;
}

export function getTheme(): Theme {
  return DEFAULT_THEME;
}

export function applyTheme(): void {
  const root = document.documentElement;
  root.setAttribute('data-theme', getTheme());
  root.setAttribute('data-density', getDensity());
}

export function setDensity(density: Density): void {
  try {
    localStorage.setItem(DENSITY_STORAGE_KEY, density);
  } catch {
    /* storage unavailable: fall back to the in-DOM attribute only */
  }
  document.documentElement.setAttribute('data-density', density);
}

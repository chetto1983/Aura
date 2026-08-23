import { afterEach, describe, expect, it } from 'vitest';
import { AURA_TO_MCP_APPS, hostContextFrom } from './hostTheme';

function root(): HTMLElement {
  return document.documentElement;
}

afterEach(() => {
  root().removeAttribute('data-theme');
  root().removeAttribute('style');
});

describe('hostContextFrom', () => {
  it('reports dark when the cockpit says dark', () => {
    root().setAttribute('data-theme', 'dark');
    expect(hostContextFrom(root()).theme).toBe('dark');
  });

  it('reports light when the cockpit says light', () => {
    root().setAttribute('data-theme', 'light');
    expect(hostContextFrom(root()).theme).toBe('light');
  });

  // The probe this replaces asked for a `dark` class. applyTheme sets an attribute and
  // never a class, so every view was told "light" even in the dark cockpit; and with no
  // attribute at all Aura is dark (DEFAULT_THEME, and :root:not([data-theme]) in
  // styles/theme.css), so absence must not fall back to light either.
  it('defaults to dark when no theme attribute is set', () => {
    expect(hostContextFrom(root()).theme).toBe('dark');
  });

  it('publishes Aura tokens under the names MCP Apps defines', () => {
    root().style.setProperty('--color-bg', '#101214');
    root().style.setProperty('--color-text', '#eef2f6');
    root().style.setProperty('--color-ring', '#8ab4f8');
    root().style.setProperty('--radius-pill', '999px');

    const { variables } = hostContextFrom(root()).styles ?? {};

    expect(variables?.['--color-background-primary']).toBe('#101214');
    expect(variables?.['--color-text-primary']).toBe('#eef2f6');
    expect(variables?.['--color-ring-primary']).toBe('#8ab4f8');
    expect(variables?.['--border-radius-full']).toBe('999px');
  });

  // A host may send any subset. Sending a name with an empty value is not the same as
  // not sending it: the empty string wins over the view's fallback and paints nothing.
  it('omits a token the cockpit has not defined rather than sending it empty', () => {
    root().style.setProperty('--color-bg', '#101214');

    const { variables } = hostContextFrom(root()).styles ?? {};

    expect(variables).toHaveProperty('--color-background-primary');
    expect(variables).not.toHaveProperty('--color-text-primary');
    expect(Object.values(variables ?? {})).not.toContain('');
  });

  it('still feeds the WhatsApp view its legacy background name', () => {
    root().style.setProperty('--color-surface', '#191c20');

    const { variables } = hostContextFrom(root()).styles ?? {};

    expect(variables?.['--wa-bg']).toBe('#191c20');
  });

  it('maps every entry to a distinct published name', () => {
    const published = AURA_TO_MCP_APPS.map(([name]) => name);
    expect(new Set(published).size).toBe(published.length);
  });
});

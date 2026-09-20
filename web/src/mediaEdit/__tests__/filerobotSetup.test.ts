import defaultTranslations from 'react-filerobot-image-editor/lib/context/defaultTranslations';
import { describe, expect, it } from 'vitest';
import {
  FILEROBOT_IT,
  filerobotTheme,
  filerobotTranslations,
  forwardDomProp,
} from '../filerobotSetup';

describe('FILEROBOT_IT', () => {
  it('covers exactly the keys Filerobot ships', () => {
    // A new Filerobot key would otherwise fall back to English without anyone noticing.
    expect(Object.keys(FILEROBOT_IT).sort()).toEqual(Object.keys(defaultTranslations).sort());
  });

  it('translates every key', () => {
    for (const [key, value] of Object.entries(FILEROBOT_IT)) {
      expect(value, key).not.toBe('');
    }
  });
});

describe('filerobotTranslations', () => {
  it('is the Italian table for Italian and the library default otherwise', () => {
    expect(filerobotTranslations('it')).toBe(FILEROBOT_IT);
    expect(filerobotTranslations('it-IT')).toBe(FILEROBOT_IT);
    expect(filerobotTranslations('en')).toBeUndefined();
  });
});

describe('filerobotTheme', () => {
  it('reads the colours and the font from the Aura tokens', () => {
    const root = document.createElement('div');
    root.style.setProperty('--color-accent', '#1f3760');
    root.style.setProperty('--color-on-accent', '#d3e3fd');
    root.style.setProperty('--color-surface', '#191c20');
    root.style.setProperty('--color-surface-3', '#303741');
    root.style.setProperty('--color-text', '#eef2f6');
    root.style.setProperty('--color-text-muted', '#c8d0d8');
    root.style.setProperty('--color-text-disabled', '#6f7377');
    root.style.setProperty('--font-sans', 'Test Sans');
    document.body.append(root);
    const theme = filerobotTheme(root);
    expect(theme.palette).toMatchObject({
      'accent-primary': '#d3e3fd',
      'accent-primary-active': '#d3e3fd',
      'accent-stateless': '#d3e3fd',
      'bg-stateless': '#303741',
      'bg-primary': '#191c20',
      'bg-primary-active': '#1f3760',
      'txt-primary': '#eef2f6',
      'icon-primary': '#c8d0d8',
      'icons-invert': '#1f3760',
      'btn-primary-text': '#1f3760',
      'btn-disabled-text': '#6f7377',
    });
    expect(theme.typography?.fontFamily).toBe('Test Sans');
    root.remove();
  });

  it('leaves out a token the page does not define', () => {
    const root = document.createElement('div');
    document.body.append(root);
    const theme = filerobotTheme(root);
    expect(theme.palette).toEqual({});
    expect(theme.typography).toBeUndefined();
    root.remove();
  });
});

describe('forwardDomProp', () => {
  it('drops styled props on DOM elements and keeps them on components', () => {
    expect(forwardDomProp('showTabsDrawer', 'div')).toBe(false);
    expect(forwardDomProp('aria-label', 'div')).toBe(true);
    expect(forwardDomProp('showTabsDrawer', () => null)).toBe(true);
  });
});

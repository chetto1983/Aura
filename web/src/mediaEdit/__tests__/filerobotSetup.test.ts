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
    root.style.setProperty('--color-accent', '#ff5500');
    root.style.setProperty('--color-surface', '#101010');
    root.style.setProperty('--font-sans', 'Test Sans');
    document.body.append(root);
    const theme = filerobotTheme(root);
    expect(theme.palette?.['accent-primary']).toBe('#ff5500');
    expect(theme.palette?.['bg-primary']).toBe('#101010');
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

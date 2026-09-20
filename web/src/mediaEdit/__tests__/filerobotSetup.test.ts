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

// Aura's dark tokens, as tokens/tokens.json generates them.
const DARK_TOKENS: Readonly<Record<string, string>> = {
  '--color-surface': '#191c20',
  '--color-surface-2': '#252a31',
  '--color-surface-3': '#303741',
  '--color-border': '#343c46',
  '--color-border-strong': '#718394',
  '--color-text': '#eef2f6',
  '--color-text-muted': '#c8d0d8',
  '--color-text-faint': '#9ba8b5',
  '--color-text-disabled': '#6f7377',
  '--color-accent': '#1f3760',
  '--color-accent-text': '#d3e3fd',
  '--color-on-accent': '#d3e3fd',
  '--color-success': '#81c995',
  '--color-warning': '#fdd663',
  '--color-danger': '#f28b82',
  '--color-info': '#aecbfa',
  '--color-ring': '#8ab4f8',
  '--font-sans': 'Test Sans',
};

function themedRoot(tokens: Readonly<Record<string, string>> = DARK_TOKENS): HTMLElement {
  const root = document.createElement('div');
  for (const [name, value] of Object.entries(tokens)) root.style.setProperty(name, value);
  document.body.append(root);
  return root;
}

// Every key the editor reads and this list does not carry keeps @scaleflex/ui's own LIGHT
// default, which is how the crop menu's chosen row once ended white-on-white in the dark
// cockpit. Dropping a key from filerobotSetup.ts has to fail here, not on someone's screen.
const PALETTE_KEYS: readonly string[] = [
  'accent-primary',
  'accent-primary-active',
  'accent-primary-disabled',
  'accent-primary-hover',
  'accent-stateless',
  'accent-stateless_0_4_opacity',
  'accent_1_2_opacity',
  'accent_1_8_opacity',
  'accent_2_8_opacity',
  'accent_4_0_opacity',
  'access-primary',
  'active-secondary',
  'bg-active',
  'bg-blue',
  'bg-green',
  'bg-grey',
  'bg-hover',
  'bg-orange',
  'bg-primary',
  'bg-primary-active',
  'bg-primary-hover',
  'bg-primary-stateless',
  'bg-red',
  'bg-red-light',
  'bg-secondary',
  'bg-stateless',
  'border-active-bottom',
  'border-hover-bottom',
  'border-primary-stateless',
  'borders-button',
  'borders-disabled',
  'borders-item',
  'borders-primary',
  'borders-primary-hover',
  'borders-secondary',
  'borders-strong',
  'btn-disabled-text',
  'btn-primary-text',
  'btn-secondary-text',
  'error',
  'extra-0-3-overlay',
  'icon-primary',
  'icons-invert',
  'icons-muted',
  'icons-placeholder',
  'icons-primary',
  'icons-primary-hover',
  'icons-secondary',
  'icons-secondary-hover',
  'info',
  'large-shadow',
  'light-shadow',
  'link-active',
  'link-hover',
  'link-primary',
  'link-stateless',
  'medium-shadow',
  'modified',
  'success',
  'txt-error',
  'txt-info',
  'txt-placeholder',
  'txt-primary',
  'txt-secondary',
  'txt-secondary-invert',
  'txt-warning',
  'warning',
  'white-0-7-8-overlay',
];

describe('filerobotTheme', () => {
  it('answers every Scaleflex key the editor reads, and no others', () => {
    const root = themedRoot();
    expect(Object.keys(filerobotTheme(root).palette).sort()).toEqual([...PALETTE_KEYS].sort());
    root.remove();
  });

  it('reads the colours and the font from the Aura tokens', () => {
    const root = themedRoot();
    const theme = filerobotTheme(root);
    expect(theme.palette).toMatchObject({
      'accent-primary': '#d3e3fd',
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

  it('answers the keys whose absence was visible on the page', () => {
    const root = themedRoot();
    const { palette } = filerobotTheme(root);
    // The chosen crop ratio, on the accent rather than on the menu's own surface.
    expect(palette['bg-active']).toBe('#1f3760');
    // The zoom and image-info icons (icons-muted), and Filerobot's global svg rule, which
    // asks for the plural spelling Scaleflex's own Color enum does not define.
    expect(palette['icons-muted']).toBe('#9ba8b5');
    expect(palette['icons-primary']).toBe('#c8d0d8');
    // Every themed field's resting border.
    expect(palette['border-primary-stateless']).toBe('#718394');
    // Filerobot's typo for the crop and transform handle fill.
    expect(palette['access-primary']).toBe('#191c20');
    root.remove();
  });

  it('derives the tints, the pressed step and the shadows', () => {
    const root = themedRoot();
    const { palette } = filerobotTheme(root);
    expect(palette.accent_1_2_opacity).toBe('rgb(211 227 253 / 0.12)');
    expect(palette['bg-red']).toBe('rgb(242 139 130 / 0.14)');
    // --color-ring a quarter of the way to --color-text.
    expect(palette['accent-primary-active']).toBe('rgb(163 196 248)');
    expect(palette['light-shadow']).toBe('rgb(0 0 0 / 0.18)');
    root.remove();
  });

  it('leaves a derived key to Scaleflex when its token is not a hex colour', () => {
    const root = themedRoot({ '--color-danger': 'rebeccapurple', '--color-on-accent': '#d3e3fd' });
    const { palette } = filerobotTheme(root);
    expect(palette['bg-red']).toBeUndefined();
    expect(palette.accent_1_2_opacity).toBe('rgb(211 227 253 / 0.12)');
    root.remove();
  });

  it('keeps only the theme-independent values when the page defines no token', () => {
    const root = document.createElement('div');
    document.body.append(root);
    const theme = filerobotTheme(root);
    expect(Object.keys(theme.palette).sort()).toEqual([
      'extra-0-3-overlay',
      'large-shadow',
      'light-shadow',
      'medium-shadow',
    ]);
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

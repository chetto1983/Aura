import { describe, expect, it } from 'vitest';
import { normalizeLanguage, supportedLanguages } from '../i18n/i18n';

describe('i18n normalizeLanguage', () => {
  it('normalizes supported locale tags to their base language', () => {
    expect(normalizeLanguage('en')).toBe('en');
    expect(normalizeLanguage('it')).toBe('it');
    expect(normalizeLanguage('it-IT')).toBe('it'); // strips the region subtag
    expect(normalizeLanguage('EN-us')).toBe('en'); // case-insensitive
    expect(normalizeLanguage('  it  ')).toBe('it'); // trims whitespace
  });

  it('returns undefined for unsupported / empty / nullish input', () => {
    expect(normalizeLanguage('fr')).toBeUndefined();
    expect(normalizeLanguage('de-DE')).toBeUndefined();
    expect(normalizeLanguage('')).toBeUndefined();
    expect(normalizeLanguage(null)).toBeUndefined();
    expect(normalizeLanguage(undefined)).toBeUndefined();
  });

  it('exposes en + it as the supported languages', () => {
    expect(supportedLanguages).toEqual(['en', 'it']);
  });
});

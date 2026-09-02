import { describe, expect, it } from 'vitest';
import { en } from '../messages/en.js';
import { it as italian } from '../messages/it.js';
import { createTranslator, detectLocale } from '../i18n.js';

describe('installer i18n', () => {
  it('keeps Italian and English catalog keys identical', () => {
    expect(Object.keys(italian).sort()).toEqual(Object.keys(en).sort());
  });

  it.each([
    ['it-IT', 'it'],
    ['it_CH', 'it'],
    ['en-US', 'en'],
    ['de-DE', 'en'],
    [undefined, 'en'],
  ] as const)('maps %s to %s', (input, expected) => {
    expect(detectLocale(input)).toBe(expected);
  });

  it('interpolates named values without evaluating content', () => {
    const t = createTranslator('it');
    expect(t('remoteTargetSummary', { user: 'pi', host: '10.0.0.5', port: 22 }))
      .toBe('Destinazione: pi@10.0.0.5:22');
  });
});

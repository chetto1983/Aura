import { afterEach, describe, expect, it } from 'vitest';
import i18n from '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { studioErrorSentence } from '../studioErrors';

// Every code A2 documents must have a sentence in BOTH locales. The bug this pins is not a
// missing string — it is a code falling through to the server's own message, which Go writes
// in English, so an Italian operator read an English sentence for a failure Aura knows.

const CODES = [
  'no_key',
  'no_credit',
  'unsupported',
  'model_rejected',
  'content_blocked',
  'asset_not_found',
  'too_large',
  'job_failed',
  'job_expired',
  'outcome_unknown',
  'local_route',
] as const;

const SERVER_SAID = 'openrouter said something in English';

afterEach(async () => {
  if (i18n.language !== 'en') await i18n.changeLanguage('en');
});

describe('studioErrorSentence', () => {
  it.each(CODES)('translates %s in English, over the server string', (code) => {
    const sentence = studioErrorSentence(i18n.t, code, SERVER_SAID);
    expect(sentence).not.toBe(SERVER_SAID);
    expect(sentence).not.toBe(`studio.error.${code}`);
    expect(sentence.length).toBeGreaterThan(10);
  });

  it('translates every documented code in Italian too', async () => {
    await i18n.changeLanguage('it');
    const english = new Set<string>();
    await i18n.changeLanguage('en');
    for (const code of CODES) english.add(studioErrorSentence(i18n.t, code, SERVER_SAID));

    await i18n.changeLanguage('it');
    for (const code of CODES) {
      const italian = studioErrorSentence(i18n.t, code, SERVER_SAID);
      expect(italian).not.toBe(SERVER_SAID);
      // A key present in `it` but copied from `en` would be a translation that is not one.
      expect(english.has(italian)).toBe(false);
    }
  });

  it('names the two refusals whose wording the operator has to act on', () => {
    expect(studioErrorSentence(i18n.t, 'outcome_unknown', '')).toContain(
      'may already have been billed',
    );
    expect(studioErrorSentence(i18n.t, 'no_key', '')).toContain('OpenRouter key');
  });

  it('shows what the server said for a code minted after this build', () => {
    expect(studioErrorSentence(i18n.t, 'quarantined', SERVER_SAID)).toBe(SERVER_SAID);
  });

  it('falls back to the generic sentence when there is no code and no message', () => {
    expect(studioErrorSentence(i18n.t, '', '   ')).toBe(
      'The Studio could not complete that request.',
    );
  });
});

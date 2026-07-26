import { describe, expect, it } from 'vitest';
import {
  credentialsValid,
  isAuthError,
  isForbiddenError,
  PHASES,
  phaseIndex,
  provisionErrorKind,
  SEED_FIELD_LIMITS,
  SEED_FIELDS,
  seedBlank,
  seedFieldTooLong,
  seedNameMissing,
  seedValid,
  stepState,
} from '../onboardingWizardModel';

// onboardingWizardModel test — the wizard's PURE decision logic, unit-tested directly so every
// branch/equality/boolean mutant is killable without the DOM (the 28-03 mutation-hardening
// playbook). These functions drive the wizard's flow gates + the stepper render.

describe('isAuthError', () => {
  it('is true ONLY for Error("HTTP 401")', () => {
    expect(isAuthError(new Error('HTTP 401'))).toBe(true);
  });
  it('is false for other HTTP codes, non-Error values, and undefined', () => {
    expect(isAuthError(new Error('HTTP 403'))).toBe(false);
    expect(isAuthError(new Error('HTTP 500'))).toBe(false);
    expect(isAuthError('HTTP 401')).toBe(false);
    expect(isAuthError(undefined)).toBe(false);
    expect(isAuthError(null)).toBe(false);
  });
});

describe('isForbiddenError', () => {
  it('is true ONLY for Error("HTTP 403")', () => {
    expect(isForbiddenError(new Error('HTTP 403'))).toBe(true);
  });
  it('is false for auth errors, other HTTP codes, and non-Error values', () => {
    expect(isForbiddenError(new Error('HTTP 401'))).toBe(false);
    expect(isForbiddenError(new Error('HTTP 500'))).toBe(false);
    expect(isForbiddenError('HTTP 403')).toBe(false);
    expect(isForbiddenError(undefined)).toBe(false);
  });
});

describe('provisionErrorKind', () => {
  it('maps 403 → noCapability', () => {
    expect(provisionErrorKind(new Error('HTTP 403'))).toBe('noCapability');
  });
  it('maps 409 → duplicate', () => {
    expect(provisionErrorKind(new Error('HTTP 409'))).toBe('duplicate');
  });
  it('maps anything else (502, 400, non-Error) → rolledBack', () => {
    expect(provisionErrorKind(new Error('HTTP 502'))).toBe('rolledBack');
    expect(provisionErrorKind(new Error('HTTP 400'))).toBe('rolledBack');
    expect(provisionErrorKind('HTTP 403')).toBe('rolledBack');
    expect(provisionErrorKind(undefined)).toBe('rolledBack');
  });
});

describe('phaseIndex / PHASES', () => {
  it('returns the 0-based position in the four-phase flow (the interview phase is gone)', () => {
    expect(PHASES).toEqual(['credentials', 'capabilities', 'review', 'complete']);
    expect(phaseIndex('credentials')).toBe(0);
    expect(phaseIndex('capabilities')).toBe(1);
    expect(phaseIndex('review')).toBe(2);
    expect(phaseIndex('complete')).toBe(3);
  });
});

describe('seedFieldTooLong / SEED_FIELD_LIMITS', () => {
  it('mirrors the server caps exactly (256 text, 32 lang, 64 timezone)', () => {
    expect(SEED_FIELD_LIMITS).toEqual({
      name: 256,
      lang: 32,
      location: 256,
      timezone: 64,
      role: 256,
      company: 256,
    });
    expect(SEED_FIELDS).toEqual(['name', 'lang', 'location', 'timezone', 'role', 'company']);
  });

  it('is false AT the cap and true one character past it, for every field', () => {
    for (const field of SEED_FIELDS) {
      const cap = SEED_FIELD_LIMITS[field];
      expect(seedFieldTooLong(field, 'x'.repeat(cap))).toBe(false);
      expect(seedFieldTooLong(field, 'x'.repeat(cap + 1))).toBe(true);
    }
  });

  it('is false for an empty value (every field is optional)', () => {
    expect(seedFieldTooLong('name', '')).toBe(false);
  });

  it('counts characters WITHOUT trimming, so trailing spaces can push a value over', () => {
    expect(seedFieldTooLong('lang', `${'x'.repeat(32)} `)).toBe(true);
  });

  // The server counts BYTES (Go's len). A UTF-16 .length would let these through here and 400
  // there, and neither surface offers a way back to the field from the error state.
  it('counts UTF-8 BYTES, exactly as the server does', () => {
    expect(seedFieldTooLong('role', `${'x'.repeat(250)}${'è'.repeat(3)}`)).toBe(false); // 256 bytes
    expect(seedFieldTooLong('role', `${'x'.repeat(250)}${'è'.repeat(4)}`)).toBe(true); // 258 bytes
    expect(seedFieldTooLong('company', '漢'.repeat(86))).toBe(true); // 258 bytes, 86 UTF-16 units
    expect(seedFieldTooLong('name', '🙂'.repeat(64))).toBe(false); // 256 bytes, 128 UTF-16 units
  });
});

describe('seedBlank / seedNameMissing', () => {
  it('treats an empty or all-whitespace seed as blank — that is the skip', () => {
    expect(seedBlank({})).toBe(true);
    expect(seedBlank({ name: '  ', role: '\t' })).toBe(true);
    expect(seedBlank({ lang: 'it' })).toBe(false);
  });

  it('requires a name once ANY other field is filled, and exempts the blank skip', () => {
    expect(seedNameMissing({})).toBe(false);
    expect(seedNameMissing({ name: 'Davide' })).toBe(false);
    expect(seedNameMissing({ name: 'Davide', role: 'founder' })).toBe(false);
    expect(seedNameMissing({ role: 'founder' })).toBe(true);
    expect(seedNameMissing({ name: '   ', company: 'PmSync' })).toBe(true);
  });
});

describe('seedValid', () => {
  it('accepts an entirely blank seed (blank is a valid submission the server derives as a skip)', () => {
    expect(seedValid({})).toBe(true);
  });
  it('accepts a fully filled seed within the caps', () => {
    expect(
      seedValid({
        name: 'Davide',
        lang: 'it',
        location: 'Caraglio',
        timezone: 'Europe/Rome',
        role: 'founder',
        company: 'PmSync',
      }),
    ).toBe(true);
  });
  it('rejects a seed with ANY field over its own cap', () => {
    expect(seedValid({ name: 'x'.repeat(257) })).toBe(false);
    expect(seedValid({ name: 'Davide', lang: 'x'.repeat(33) })).toBe(false);
    expect(seedValid({ name: 'Davide', timezone: 'x'.repeat(65) })).toBe(false);
    // A 33-character NAME is fine — the caps are per-field, not shared.
    expect(seedValid({ name: 'x'.repeat(33) })).toBe(true);
  });
  // Without a name the mapper keys every fact off the placeholder subject "operator" and writes
  // no PERSON entity for them to resolve against — the orphan shape Amendment #95 exists to end.
  it('rejects a filled seed that carries no name', () => {
    expect(seedValid({ role: 'founder', company: 'PmSync' })).toBe(false);
    expect(seedValid({ name: '  ', location: 'Caraglio' })).toBe(false);
  });
});

describe('credentialsValid', () => {
  it('requires email, password, matching confirmation, security question, and security answer', () => {
    expect(credentialsValid('a@b.com', 'pw', 'pw', 'Question?', 'answer')).toBe(true);
    expect(credentialsValid('a@b.com', 'pw', 'pw', '', 'answer')).toBe(false);
    expect(credentialsValid('a@b.com', 'pw', 'pw', 'Question?', '')).toBe(false);
  });
  it('is false when email is empty/whitespace', () => {
    expect(credentialsValid('', 'pw', 'pw', 'Question?', 'answer')).toBe(false);
    expect(credentialsValid('   ', 'pw', 'pw', 'Question?', 'answer')).toBe(false);
  });
  it('is false when password or confirmation is empty', () => {
    expect(credentialsValid('a@b.com', '', '', 'Question?', 'answer')).toBe(false);
    expect(credentialsValid('a@b.com', 'pw', '', 'Question?', 'answer')).toBe(false);
  });
  it('is false when password and confirmation differ', () => {
    expect(credentialsValid('a@b.com', 'pw', 'different', 'Question?', 'answer')).toBe(false);
  });
  it('does NOT trim the password (a whitespace password is accepted as entered)', () => {
    expect(credentialsValid('a@b.com', '   ', '   ', 'Question?', 'answer')).toBe(true);
  });
});

describe('stepState', () => {
  it('classifies done / active / upcoming relative to the active index', () => {
    expect(stepState(0, 2)).toBe('done');
    expect(stepState(1, 2)).toBe('done');
    expect(stepState(2, 2)).toBe('active');
    expect(stepState(3, 2)).toBe('upcoming');
    expect(stepState(4, 2)).toBe('upcoming');
  });
});

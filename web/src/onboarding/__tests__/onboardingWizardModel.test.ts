import { describe, expect, it } from 'vitest';
import {
  credentialsValid,
  isAuthError,
  isForbiddenError,
  isTerminalStatus,
  PHASES,
  phaseIndex,
  provisionErrorKind,
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

describe('isTerminalStatus', () => {
  it('is true for completed / skipped / canceled', () => {
    expect(isTerminalStatus('completed')).toBe(true);
    expect(isTerminalStatus('skipped')).toBe(true);
    expect(isTerminalStatus('canceled')).toBe(true);
  });
  it('is false for active / draft / unknown', () => {
    expect(isTerminalStatus('active')).toBe(false);
    expect(isTerminalStatus('draft')).toBe(false);
    expect(isTerminalStatus('')).toBe(false);
    expect(isTerminalStatus('completedX')).toBe(false);
  });
});

describe('phaseIndex / PHASES', () => {
  it('returns the 0-based position in the linear flow', () => {
    expect(PHASES).toEqual(['credentials', 'capabilities', 'interview', 'review', 'complete']);
    expect(phaseIndex('credentials')).toBe(0);
    expect(phaseIndex('capabilities')).toBe(1);
    expect(phaseIndex('interview')).toBe(2);
    expect(phaseIndex('review')).toBe(3);
    expect(phaseIndex('complete')).toBe(4);
  });
});

describe('credentialsValid', () => {
  it('is true only when BOTH email (trimmed non-empty) and password are present', () => {
    expect(credentialsValid('a@b.com', 'pw')).toBe(true);
  });
  it('is false when email is empty/whitespace', () => {
    expect(credentialsValid('', 'pw')).toBe(false);
    expect(credentialsValid('   ', 'pw')).toBe(false);
  });
  it('is false when password is empty', () => {
    expect(credentialsValid('a@b.com', '')).toBe(false);
  });
  it('does NOT trim the password (a whitespace password is accepted as entered)', () => {
    expect(credentialsValid('a@b.com', '   ')).toBe(true);
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

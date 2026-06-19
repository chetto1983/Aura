import { describe, expect, it } from 'vitest';
import { hasField, hasOptions, isSwarmStatus, statusDotClass, statusLabelKey } from '../swarmRow';

// Pure swarm-row helpers — exact-output assertions pinning the status mapping and
// field-presence logic (mutation-resistant).

describe('isSwarmStatus', () => {
  it('accepts the three enum values and rejects others', () => {
    expect(isSwarmStatus('ok')).toBe(true);
    expect(isSwarmStatus('failed')).toBe(true);
    expect(isSwarmStatus('needs_user_input')).toBe(true);
    expect(isSwarmStatus('weird')).toBe(false);
    expect(isSwarmStatus('')).toBe(false);
  });
});

describe('statusDotClass', () => {
  it('maps each enum status to its semantic dot color', () => {
    expect(statusDotClass('ok')).toBe('bg-success');
    expect(statusDotClass('failed')).toBe('bg-danger');
    expect(statusDotClass('needs_user_input')).toBe('bg-warning');
  });

  it('falls to the danger dot for an out-of-enum status', () => {
    expect(statusDotClass('mystery')).toBe('bg-danger');
  });
});

describe('statusLabelKey', () => {
  it('maps each enum status to its label key', () => {
    expect(statusLabelKey('ok')).toBe('swarm.status.ok');
    expect(statusLabelKey('failed')).toBe('swarm.status.failed');
    expect(statusLabelKey('needs_user_input')).toBe('swarm.status.needs_user_input');
  });

  it('falls to the unknown key for an out-of-enum status', () => {
    expect(statusLabelKey('mystery')).toBe('swarm.status.unknown');
  });
});

describe('hasField', () => {
  it('is true only for a present, non-empty string', () => {
    expect(hasField('x')).toBe(true);
    expect(hasField('')).toBe(false);
    expect(hasField(undefined)).toBe(false);
  });
});

describe('hasOptions', () => {
  it('is true only for a present, non-empty array', () => {
    expect(hasOptions(['a'])).toBe(true);
    expect(hasOptions([])).toBe(false);
    expect(hasOptions(undefined)).toBe(false);
  });
});

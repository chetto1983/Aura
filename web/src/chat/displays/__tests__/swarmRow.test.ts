import { describe, expect, it } from 'vitest';
import {
  hasField,
  hasOptions,
  isTerminalSwarmStatus,
  isSwarmStatus,
  statusDotClass,
  statusIconName,
  statusLabelKey,
} from '../swarmRow';

// Pure swarm-row helpers — exact-output assertions pinning the status mapping and
// field-presence logic (mutation-resistant).

describe('isSwarmStatus', () => {
  it('accepts exactly the six server values and rejects the job-row spelling', () => {
    const accepted = ['ok', 'failed', 'needs_user_input', 'running', 'stalled', 'dead_letter'];
    expect(accepted.every(isSwarmStatus)).toBe(true);
    expect(isSwarmStatus('awaiting_input')).toBe(false);
    expect(isSwarmStatus('weird')).toBe(false);
  });
});

describe('statusDotClass', () => {
  it('maps each enum status to its semantic dot color', () => {
    expect(statusDotClass('ok')).toBe('bg-success');
    expect(statusDotClass('failed')).toBe('bg-danger');
    expect(statusDotClass('needs_user_input')).toBe('bg-warning');
    expect(statusDotClass('running')).toBe('bg-info');
    expect(statusDotClass('stalled')).toBe('bg-warning');
    expect(statusDotClass('dead_letter')).toBe('bg-danger');
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
    expect(statusLabelKey('running')).toBe('swarm.status.running');
    expect(statusLabelKey('stalled')).toBe('swarm.status.stalled');
    expect(statusLabelKey('dead_letter')).toBe('swarm.status.dead_letter');
  });

  it('falls to the unknown key for an out-of-enum status', () => {
    expect(statusLabelKey('mystery')).toBe('swarm.status.unknown');
  });
});

describe('statusIconName', () => {
  it('maps every state and fallback to a distinct non-colour signal', () => {
    expect(statusIconName('ok')).toBe('CircleCheck');
    expect(statusIconName('failed')).toBe('CircleX');
    expect(statusIconName('needs_user_input')).toBe('MessageCircleQuestion');
    expect(statusIconName('running')).toBe('LoaderCircle');
    expect(statusIconName('stalled')).toBe('Clock');
    expect(statusIconName('dead_letter')).toBe('MailX');
    expect(statusIconName('mystery')).toBe('TriangleAlert');
  });
});

describe('isTerminalSwarmStatus', () => {
  it('accepts only completed, failed, and dead-letter workers', () => {
    expect(isTerminalSwarmStatus('ok')).toBe(true);
    expect(isTerminalSwarmStatus('failed')).toBe(true);
    expect(isTerminalSwarmStatus('dead_letter')).toBe(true);
    expect(isTerminalSwarmStatus('needs_user_input')).toBe(false);
    expect(isTerminalSwarmStatus('running')).toBe(false);
    expect(isTerminalSwarmStatus('stalled')).toBe(false);
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

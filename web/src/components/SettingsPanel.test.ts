import { describe, expect, it } from 'vitest';
import { computeSourceBadge } from './SettingsPanel';

const t = (key: string) => key;

const base = {
  read_only: false,
  restart_required: false,
  source: 'db' as const,
  is_secret: false,
  active_value: '',
};

describe('computeSourceBadge', () => {
  it('returns "saved" badge for db source with non-secret item', () => {
    const badge = computeSourceBadge({ ...base, is_secret: false, active_value: '' }, false, t);
    expect(badge.label).toBe('settings.badge.saved');
  });

  it('returns "saved" badge for db source with is_secret=true and non-empty active_value', () => {
    const badge = computeSourceBadge({ ...base, is_secret: true, active_value: 'sk-abc123' }, false, t);
    expect(badge.label).toBe('settings.badge.saved');
  });

  it('returns "unset" badge for db source with is_secret=true and empty active_value', () => {
    const badge = computeSourceBadge({ ...base, is_secret: true, active_value: '' }, false, t);
    expect(badge.label).toBe('settings.badge.unset');
  });

  it('returns "edited" badge when dirty, regardless of is_secret state', () => {
    const badge = computeSourceBadge({ ...base, is_secret: true, active_value: '' }, true, t);
    expect(badge.label).toBe('settings.badge.edited');
  });

  it('returns "unset" badge for default source', () => {
    const badge = computeSourceBadge({ ...base, source: 'default' as 'db', is_secret: false, active_value: '' }, false, t);
    expect(badge.label).toBe('settings.badge.unset');
  });

  it('returns "env" badge for env source', () => {
    const badge = computeSourceBadge({ ...base, source: 'env' as const, is_secret: false, active_value: '' }, false, t);
    expect(badge.label).toBe('settings.badge.env');
  });

  it('returns "restart" badge when restart_required is true (not dirty)', () => {
    const badge = computeSourceBadge({ ...base, restart_required: true, source: 'db', active_value: 'old' }, false, t);
    expect(badge.label).toBe('settings.badge.restart');
  });

  it('returns "edited" badge (not restart) when both dirty and restart_required', () => {
    // dirty takes priority over restart_required in computeSourceBadge
    const badge = computeSourceBadge({ ...base, restart_required: true, source: 'db', active_value: 'old' }, true, t);
    expect(badge.label).toBe('settings.badge.edited');
  });
});

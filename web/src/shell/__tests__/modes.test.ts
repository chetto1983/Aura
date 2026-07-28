import { describe, expect, it } from 'vitest';
import { ADMIN_MODES, MODES, isAdminMode, visibleModes } from '../modes';

describe('admin-mode gating (MUSR-01 / D-03)', () => {
  it('marks settings + governance as admin-only', () => {
    expect(ADMIN_MODES).toContain('settings');
    expect(ADMIN_MODES).toContain('governance');
    expect(isAdminMode('settings')).toBe(true);
    expect(isAdminMode('governance')).toBe(true);
    expect(isAdminMode('chat')).toBe(false);
    expect(isAdminMode('documents')).toBe(false);
  });

  it('keeps the full mode list for an admin', () => {
    expect(visibleModes(MODES, true)).toEqual([...MODES]);
  });

  it('lists only surfaces that exist — no disabled placeholders', () => {
    // 'tree' and 'displays' were disabled tabs for as long as they were listed. A nav
    // entry is a claim that something is there; these never were.
    expect(MODES).not.toContain('tree');
    expect(MODES).not.toContain('displays');
  });

  it('drops the admin-only surfaces for a non-admin', () => {
    const desktop = visibleModes(MODES, false);
    expect(desktop).not.toContain('settings');
    expect(desktop).not.toContain('governance');
    expect(desktop).toContain('chat');

    expect(desktop).toContain('documents');
  });
});

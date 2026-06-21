import { describe, expect, it } from 'vitest';
import { resources } from '../resources';

// i18n parity gate — every key present in the `en` bundle MUST be present in `it` (and vice
// versa). A missing key in either locale is a defect (the resources.governance.ts discipline,
// extended to the whole bundle). This is the Phase-29 key-diff acceptance: zero missing keys
// in either locale across the governance write copy + the approval skill-install copy.

function flatten(obj: unknown, prefix = ''): string[] {
  if (obj === null || typeof obj !== 'object') return [prefix];
  return Object.entries(obj as Record<string, unknown>).flatMap(([key, value]) => {
    const next = prefix === '' ? key : `${prefix}.${key}`;
    return flatten(value, next);
  });
}

describe('i18n bundle parity (en ↔ it)', () => {
  const enKeys = new Set(flatten(resources.en.translation));
  const itKeys = new Set(flatten(resources.it.translation));

  it('has zero keys present in en but missing in it', () => {
    const missing = [...enKeys].filter((key) => !itKeys.has(key));
    expect(missing).toEqual([]);
  });

  it('has zero keys present in it but missing in en', () => {
    const missing = [...itKeys].filter((key) => !enKeys.has(key));
    expect(missing).toEqual([]);
  });

  it('includes the Phase-29 governance write + skill-approval keys in BOTH locales', () => {
    const required = [
      'governance.mcp.install.submit',
      'governance.mcp.env.softWarning.heading',
      'governance.mcp.lifecycle.removeConfirm',
      'governance.skills.install.riskBanner',
      'governance.skills.install.submit',
      'governance.skills.collidingRestore',
      'approval.skill.riskBadge',
      'approval.skill.containerNote',
      'approval.skill.resumeToken',
    ];
    for (const key of required) {
      expect(enKeys.has(key)).toBe(true);
      expect(itKeys.has(key)).toBe(true);
    }
  });
});

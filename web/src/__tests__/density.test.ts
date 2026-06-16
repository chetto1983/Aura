import { describe, expect, it } from 'vitest';
import { DEFAULT_DENSITY, DENSITIES, isDensity } from '../theme/density';

describe('density', () => {
  it('exposes the operator-console density scale with operator as default', () => {
    expect(DENSITIES).toEqual(['compact', 'operator', 'review']);
    expect(DEFAULT_DENSITY).toBe('operator');
  });

  it('isDensity accepts the known densities and rejects everything else', () => {
    for (const d of DENSITIES) {
      expect(isDensity(d)).toBe(true);
    }
    expect(isDensity('cozy')).toBe(false);
    expect(isDensity('')).toBe(false);
    expect(isDensity(null)).toBe(false);
  });
});

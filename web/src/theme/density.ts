export const DENSITIES = ['compact', 'operator', 'review'] as const;

export type Density = (typeof DENSITIES)[number];

export const DEFAULT_DENSITY: Density = 'operator';

export function isDensity(value: string | null): value is Density {
  return value !== null && (DENSITIES as readonly string[]).includes(value);
}

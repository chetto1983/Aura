import { describe, expect, it } from 'vitest';
import {
  MINIMUM_CPU_CORES,
  MINIMUM_MEMORY_KB,
  assertSufficientCpuCores,
  assertSufficientMemory,
} from '../preflight.js';

describe('installer preflight', () => {
  it('accepts a CPU count at or above the floor and rejects below it', () => {
    expect(assertSufficientCpuCores(String(MINIMUM_CPU_CORES))).toBe(MINIMUM_CPU_CORES);
    expect(assertSufficientCpuCores('16')).toBe(16);
    expect(() => assertSufficientCpuCores(String(MINIMUM_CPU_CORES - 1))).toThrow(
      `insufficientCpuCores:${MINIMUM_CPU_CORES - 1}`,
    );
  });

  it.each(['', 'four', '-4', '4.5'])('rejects a malformed CPU count %s', (value) => {
    expect(() => assertSufficientCpuCores(value)).toThrow('invalidCpuCount');
  });

  it('accepts a memory reading at or above the floor and rejects below it', () => {
    expect(assertSufficientMemory(String(MINIMUM_MEMORY_KB))).toBe(MINIMUM_MEMORY_KB);
    expect(assertSufficientMemory(String(MINIMUM_MEMORY_KB * 2))).toBe(MINIMUM_MEMORY_KB * 2);
    expect(() => assertSufficientMemory(String(MINIMUM_MEMORY_KB - 1))).toThrow(
      `insufficientMemory:${MINIMUM_MEMORY_KB - 1}`,
    );
  });

  it.each(['', 'lots', '-16777216', '16.5'])('rejects a malformed memory reading %s', (value) => {
    expect(() => assertSufficientMemory(value)).toThrow('invalidMemoryAvailability');
  });
});

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

  // Deliberately absolute, not derived from MINIMUM_MEMORY_KB: the test above compares the
  // constant against itself and would pass for ANY value of it, including one no real
  // machine can reach. MemTotal is installed RAM minus firmware, kernel and integrated-GPU
  // reservations, so these are the readings that decide whether the floor admits the
  // hardware this product targets -- and it has guessed wrong twice. The floor was 16 GiB,
  // which no 16 GB box can reach, then 15 GiB, which the appliance this project actually
  // runs on still could not reach.
  it('accepts the real 16 GB boxes it targets and still refuses an 8 GB one', () => {
    expect(assertSufficientMemory('16268000')).toBe(16268000); // ~15.51 GiB, a 16 GB box
    expect(assertSufficientMemory('15610852')).toBe(15610852); // 14.89 GiB, GEEKOM Mini Air12
    expect(() => assertSufficientMemory('8120000')).toThrow('insufficientMemory:8120000');
  });
});

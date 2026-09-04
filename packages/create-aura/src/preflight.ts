import {
  MINIMUM_CPU_CORES,
  MINIMUM_DISK_KB,
  MINIMUM_MEMORY_KB,
} from './preflight-floors.js';

// The floors are deliberately NOT written here. scripts/install.sh owns them -- it has to,
// since preflight_hw runs before any download and `curl | bash` ships that one file -- and
// preflight-floors.ts is generated from it. This wizard has to refuse exactly what the
// installer it drives refuses: greenlighting a transfer install.sh then aborts is the whole
// failure mode these checks exist to prevent, and a hand-kept second copy of the numbers
// drifted the first time a floor moved. install.sh's warn_mem/warn_disk are still not
// mirrored, on purpose: blocking where the installer merely warns is worse than silence.
export { MINIMUM_CPU_CORES, MINIMUM_DISK_KB, MINIMUM_MEMORY_KB };

export type SupportedArchitecture = 'arm64' | 'amd64';

export function normalizeArchitecture(raw: string): SupportedArchitecture {
  const architecture = raw.trim().toLowerCase();
  switch (architecture) {
    case 'aarch64':
    case 'arm64':
      return 'arm64';
    case 'x86_64':
    case 'amd64':
      return 'amd64';
    default:
      throw new Error(`unsupportedArchitecture:${architecture}`);
  }
}

export function assertSufficientDiskSpace(raw: string): number {
  const value = raw.trim();
  if (!/^\d+$/.test(value)) throw new Error('invalidDiskAvailability');
  const availableDiskKb = Number(value);
  if (!Number.isSafeInteger(availableDiskKb) || availableDiskKb < MINIMUM_DISK_KB) {
    throw new Error(`insufficientDiskSpace:${value}`);
  }
  return availableDiskKb;
}

export function assertSufficientCpuCores(raw: string): number {
  const value = raw.trim();
  if (!/^\d+$/.test(value)) throw new Error('invalidCpuCount');
  const availableCpuCores = Number(value);
  if (!Number.isSafeInteger(availableCpuCores) || availableCpuCores < MINIMUM_CPU_CORES) {
    throw new Error(`insufficientCpuCores:${value}`);
  }
  return availableCpuCores;
}

export function assertSufficientMemory(raw: string): number {
  const value = raw.trim();
  if (!/^\d+$/.test(value)) throw new Error('invalidMemoryAvailability');
  const availableMemoryKb = Number(value);
  if (!Number.isSafeInteger(availableMemoryKb) || availableMemoryKb < MINIMUM_MEMORY_KB) {
    throw new Error(`insufficientMemory:${value}`);
  }
  return availableMemoryKb;
}

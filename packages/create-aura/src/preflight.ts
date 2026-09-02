export type SupportedArchitecture = 'arm64' | 'amd64';
// scripts/install.sh's hard_disk threshold is 20 GiB (20 * 1024 * 1024 KiB) -- the shell
// installer aborts below it, so this check must fail at the same floor or the wizard would
// let an operator start a transfer that install.sh then refuses anyway.
export const MINIMUM_DISK_KB = 20 * 1024 * 1024;

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

// scripts/install.sh's hard gate is `cpus < 4` -- the shell installer aborts below it, so
// this check must fail at the same floor or the wizard would greenlight a transfer
// install.sh then refuses anyway.
export const MINIMUM_CPU_CORES = 4;

export function assertSufficientCpuCores(raw: string): number {
  const value = raw.trim();
  if (!/^\d+$/.test(value)) throw new Error('invalidCpuCount');
  const availableCpuCores = Number(value);
  if (!Number.isSafeInteger(availableCpuCores) || availableCpuCores < MINIMUM_CPU_CORES) {
    throw new Error(`insufficientCpuCores:${value}`);
  }
  return availableCpuCores;
}

// scripts/install.sh's hard_mem threshold, which is 15 GiB and not 16 for a reason worth
// keeping: the number is compared against MemTotal, which is installed RAM minus firmware
// and kernel reservations, so a 16 GB machine reports ~15.4-15.8 GiB and can never clear a
// 16 GiB floor. This constant was 16 GiB and would have refused every 16 GB box the product
// targets; do not "correct" it back. install.sh's warn_mem (32 GiB) is deliberately not
// mirrored -- the installer only refuses on the hard floor, and a wizard that blocks where
// install.sh would merely warn is worse than one that says nothing.
export const MINIMUM_MEMORY_KB = 15 * 1024 * 1024;

export function assertSufficientMemory(raw: string): number {
  const value = raw.trim();
  if (!/^\d+$/.test(value)) throw new Error('invalidMemoryAvailability');
  const availableMemoryKb = Number(value);
  if (!Number.isSafeInteger(availableMemoryKb) || availableMemoryKb < MINIMUM_MEMORY_KB) {
    throw new Error(`insufficientMemory:${value}`);
  }
  return availableMemoryKb;
}

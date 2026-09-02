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

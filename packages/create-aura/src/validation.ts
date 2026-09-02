import { isIP } from 'node:net';
import { posix } from 'node:path';

export class ValidationError extends Error {
  constructor(public readonly code: string) {
    super(code);
    this.name = 'ValidationError';
  }
}

const hostnamePattern = /^(?=.{1,253}$)(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)(?:\.(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?))*$/;
const usernamePattern = /^[a-z_][a-z0-9_-]{0,31}$/;

export function validateHost(raw: string): string {
  const value = raw.trim();
  if (!isIP(value) && !hostnamePattern.test(value)) {
    throw new ValidationError('invalidHost');
  }
  return value;
}

export function validatePort(raw: string): number {
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) throw new ValidationError('invalidPort');
  const value = Number(trimmed);
  if (!Number.isInteger(value) || value < 1 || value > 65535) {
    throw new ValidationError('invalidPort');
  }
  return value;
}

export function validateUsername(raw: string): string {
  const value = raw.trim();
  if (!usernamePattern.test(value)) throw new ValidationError('invalidUsername');
  return value;
}

export function validateInstallDir(raw: string): string {
  if ([...raw].some((character) => {
    const code = character.codePointAt(0) ?? 0;
    return code <= 31 || code === 127;
  })) {
    throw new ValidationError('invalidInstallDir');
  }
  const normalized = posix.normalize(raw.trim());
  const value = normalized.length > 1 ? normalized.replace(/\/$/, '') : normalized;
  if (!value.startsWith('/') || value === '/') {
    throw new ValidationError('invalidInstallDir');
  }
  return value;
}

// A newline reaches set_env_value, which writes two .env lines; install.sh's reader takes
// the first and docker compose takes the last, so the installer and the running appliance
// would trust different secrets. install.sh rejects it too -- this layer can say so while
// the operator is still typing. Checked on the raw value (not the trimmed one), mirroring
// validateInstallDir's control-character check above, since trimming could hide a break
// sitting at the very edge of the input.
export function assertNoLineBreak(raw: string, code: string): void {
  if (/[\n\r]/.test(raw)) throw new ValidationError(code);
}

export function validateBaseUrl(raw: string): string {
  assertNoLineBreak(raw, 'invalidBaseUrl');
  const value = raw.trim();
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new ValidationError('invalidBaseUrl');
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new ValidationError('invalidBaseUrl');
  }
  return value;
}

// Aura is agnostic about which model an operator runs: no vendor-specific naming shape is
// enforced here, only that a value was actually given and carries no line break.
export function validateModelId(raw: string): string {
  assertNoLineBreak(raw, 'invalidModelId');
  const value = raw.trim();
  if (value.length === 0) throw new ValidationError('invalidModelId');
  return value;
}

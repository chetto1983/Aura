import { describe, expect, it } from 'vitest';
import {
  assertNoLineBreak,
  validateHost,
  validateInstallDir,
  validatePort,
  validateUsername,
} from '../validation.js';

describe('installer validation', () => {
  it.each(['192.168.1.20', 'raspberrypi.local', 'aura-edge-01', '2001:db8::10'])(
    'accepts host %s',
    (value) => {
      expect(validateHost(value)).toBe(value);
    },
  );

  it.each(['', 'bad host', '-invalid.local', 'host;reboot'])('rejects host %s', (value) => {
    expect(() => validateHost(value)).toThrow('invalidHost');
  });

  it('normalizes and validates the remaining target values', () => {
    expect(validatePort('22')).toBe(22);
    expect(validateUsername('pi')).toBe('pi');
    expect(validateInstallDir('/opt/aura/')).toBe('/opt/aura');
  });

  it.each(['/', 'relative/path', '/opt/aura\nother', '/opt/aura\0other'])(
    'rejects unsafe install path %s',
    (value) => {
      expect(() => validateInstallDir(value)).toThrow('invalidInstallDir');
    },
  );

  // A newline reaches set_env_value, which writes two .env lines; install.sh's reader takes
  // the first and docker compose takes the last, so the installer and the running appliance
  // would trust different values. install.sh rejects it too -- this layer says so first.
  it('assertNoLineBreak passes clean values and throws the caller-supplied code otherwise', () => {
    expect(() => assertNoLineBreak('clean', 'invalidConfigValue')).not.toThrow();
    expect(() => assertNoLineBreak('a\r\nb', 'invalidConfigValue')).toThrow('invalidConfigValue');
  });
});

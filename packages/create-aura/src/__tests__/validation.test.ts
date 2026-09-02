import { describe, expect, it } from 'vitest';
import {
  ValidationError,
  assertNoLineBreak,
  validateBaseUrl,
  validateHost,
  validateInstallDir,
  validateModelId,
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

  it.each(['http://10.0.0.5:11434', 'https://openrouter.ai/api/v1'])(
    'accepts base url %s',
    (value) => {
      expect(validateBaseUrl(value)).toBe(value);
    },
  );

  it.each(['', 'not a url', 'ftp://10.0.0.5', 'javascript:alert(1)'])(
    'rejects base url %s',
    (value) => {
      expect(() => validateBaseUrl(value)).toThrow('invalidBaseUrl');
    },
  );

  it('accepts an opaque model id and rejects an empty one', () => {
    expect(validateModelId('deepseek/deepseek-v4')).toBe('deepseek/deepseek-v4');
    expect(() => validateModelId('   ')).toThrow('invalidModelId');
  });

  // A newline reaches set_env_value, which writes two .env lines; install.sh's reader takes
  // the first and docker compose takes the last, so the installer and the running appliance
  // would trust different secrets. install.sh rejects it too -- this layer can say so while
  // the operator is still typing.
  it('rejects a line break in a model id', () =>
    expect(() => validateModelId('a\nOPENROUTER_API_KEY=x')).toThrow(ValidationError));

  it('rejects a line break in a base url', () =>
    expect(() => validateBaseUrl('http://10.0.0.5\nOPENROUTER_API_KEY=x')).toThrow(ValidationError));

  it('assertNoLineBreak passes clean values and throws the caller-supplied code otherwise', () => {
    expect(() => assertNoLineBreak('clean', 'invalidModelId')).not.toThrow();
    expect(() => assertNoLineBreak('a\r\nb', 'invalidModelId')).toThrow('invalidModelId');
  });
});

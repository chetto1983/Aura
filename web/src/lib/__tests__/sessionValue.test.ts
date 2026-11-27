import { afterEach, describe, expect, it, vi } from 'vitest';
import { readSessionValue, writeSessionValue } from '../sessionValue';

describe('sessionValue', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('writes, reads and removes a value', () => {
    expect(readSessionValue('k')).toBeUndefined();
    writeSessionValue('k', 'v');
    expect(readSessionValue('k')).toBe('v');
    expect(sessionStorage.getItem('k')).toBe('v');
    writeSessionValue('k', undefined);
    expect(readSessionValue('k')).toBeUndefined();
    expect(sessionStorage.getItem('k')).toBeNull();
  });

  // Private mode and blocked site data refuse storage by throwing.
  it('reads nothing and writes nothing when the browser refuses storage', () => {
    const refuse = () => {
      throw new DOMException('denied', 'SecurityError');
    };
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(refuse);
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(refuse);
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(refuse);
    expect(readSessionValue('k')).toBeUndefined();
    expect(() => {
      writeSessionValue('k', 'v');
      writeSessionValue('k', undefined);
    }).not.toThrow();
  });
});

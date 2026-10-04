import { afterEach, describe, expect, it } from 'vitest';
import { isIOSHomeScreenApp } from '../installedApp';

function standalone(value: boolean): void {
  Object.defineProperty(navigator, 'standalone', { value, configurable: true });
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'standalone');
});

describe('isIOSHomeScreenApp', () => {
  it('is false in a browser, which has no navigator.standalone at all', () => {
    expect(isIOSHomeScreenApp()).toBe(false);
  });

  it('is true when iOS runs the cockpit from the home screen', () => {
    standalone(true);
    expect(isIOSHomeScreenApp()).toBe(true);
  });

  it('is false in Safari itself, where the flag exists and is false', () => {
    standalone(false);
    expect(isIOSHomeScreenApp()).toBe(false);
  });
});

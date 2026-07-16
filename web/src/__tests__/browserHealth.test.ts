import { describe, expect, it } from 'vitest';
import type { Page, Request } from '@playwright/test';
import { collectBrowserHealth } from '../../e2e/support/browserHealth';

class FakePage {
  private readonly listeners = new Map<string, ((value: unknown) => void)[]>();

  on(event: string, listener: (value: unknown) => void) {
    const listeners = this.listeners.get(event) ?? [];
    listeners.push(listener);
    this.listeners.set(event, listeners);
    return this;
  }

  emit(event: string, value: unknown) {
    for (const listener of this.listeners.get(event) ?? []) listener(value);
  }
}

function failedRequest(url: string, method: string, errorText: string): Request {
  return {
    url: () => url,
    method: () => method,
    failure: () => ({ errorText }),
  } as unknown as Request;
}

describe('collectBrowserHealth request failures', () => {
  it('rejects an unarmed cross-origin GET whose URL only looks like an agent-run abort', () => {
    const page = new FakePage();
    const health = collectBrowserHealth(page as unknown as Page, 'http://aura.test');

    page.emit(
      'requestfailed',
      failedRequest('http://untrusted.test/agent/run', 'GET', 'net::ERR_ABORTED'),
    );

    expect(() => {
      health.assertClean();
    }).toThrow(/requestfailed: GET http:\/\/untrusted\.test\/agent\/run net::ERR_ABORTED/);
  });

  it('consumes exactly one precisely armed same-origin request failure', () => {
    const page = new FakePage();
    const health = collectBrowserHealth(page as unknown as Page, 'http://aura.test');
    health.expectRequestFailure({
      method: 'POST',
      pathname: '/agent/run',
      errorText: 'net::ERR_ABORTED',
    });

    page.emit(
      'requestfailed',
      failedRequest('http://aura.test/agent/run', 'POST', 'net::ERR_ABORTED'),
    );
    expect(() => {
      health.assertClean();
    }).not.toThrow();

    page.emit(
      'requestfailed',
      failedRequest('http://aura.test/agent/run', 'POST', 'net::ERR_ABORTED'),
    );
    expect(() => {
      health.assertClean();
    }).toThrow(/requestfailed: POST http:\/\/aura\.test\/agent\/run net::ERR_ABORTED/);
  });
});

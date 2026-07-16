import { expect, type Page } from '@playwright/test';

export interface AllowedHttpFailure {
  readonly method: string;
  readonly pathname: string;
  readonly status: number;
}

export interface ExpectedRequestFailure {
  readonly method: string;
  readonly pathname: string;
  readonly errorText: string;
}

export function collectBrowserHealth(
  page: Page,
  origin: string,
  allowedHttp: readonly AllowedHttpFailure[] = [],
) {
  const expectedOrigin = new URL(origin).origin;
  const problems: string[] = [];
  const consoleProblems: { readonly text: string; readonly url: string }[] = [];
  const allowedObserved = new Set<string>();
  const expectedRequestFailures: ExpectedRequestFailure[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleProblems.push({ text: message.text(), url: message.location().url });
    }
  });
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
  page.on('requestfailed', (request) => {
    const failure = request.failure()?.errorText ?? 'unknown';
    let url: URL;
    try {
      url = new URL(request.url());
    } catch {
      problems.push(`requestfailed: ${request.method()} ${request.url()} ${failure}`);
      return;
    }
    const expectedIndex = expectedRequestFailures.findIndex(
      (entry) =>
        url.origin === expectedOrigin &&
        entry.method === request.method() &&
        entry.pathname === url.pathname &&
        entry.errorText === failure,
    );
    if (expectedIndex >= 0) {
      expectedRequestFailures.splice(expectedIndex, 1);
      return;
    }
    problems.push(`requestfailed: ${request.method()} ${request.url()} ${failure}`);
  });
  page.on('response', (response) => {
    const url = new URL(response.url());
    const request = response.request();
    const allowed =
      url.origin === expectedOrigin &&
      allowedHttp.some(
        (entry) =>
          entry.method === request.method() &&
          entry.pathname === url.pathname &&
          entry.status === response.status(),
      );
    if (allowed) {
      allowedObserved.add(`${request.method()} ${url.href} ${String(response.status())}`);
    }
    if (url.origin === expectedOrigin && response.status() >= 400 && !allowed) {
      problems.push(`http: ${String(response.status())} ${url.pathname}`);
    }
  });
  return {
    expectRequestFailure(expected: ExpectedRequestFailure) {
      expectedRequestFailures.push(expected);
    },
    assertClean() {
      const unallowedConsole = consoleProblems.filter((problem) => {
        let url: URL;
        try {
          url = new URL(problem.url);
        } catch {
          return true;
        }
        return !allowedHttp.some(
          (entry) =>
            url.origin === expectedOrigin &&
            entry.pathname === url.pathname &&
            allowedObserved.has(`${entry.method} ${url.href} ${String(entry.status)}`) &&
            problem.text ===
              'Failed to load resource: the server responded with a status of 409 (Conflict)',
        );
      });
      expect([
        ...problems,
        ...expectedRequestFailures.map(
          (entry) =>
            `missing requestfailed: ${entry.method} ${expectedOrigin}${entry.pathname} ${entry.errorText}`,
        ),
        ...unallowedConsole.map((problem) => `console: ${problem.text}`),
      ]).toEqual([]);
    },
  };
}

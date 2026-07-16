import { expect, type Page } from '@playwright/test';

export interface AllowedHttpFailure {
  readonly method: string;
  readonly pathname: string;
  readonly status: number;
}

export function collectBrowserHealth(
  page: Page,
  origin: string,
  allowedHttp: readonly AllowedHttpFailure[] = [],
) {
  const problems: string[] = [];
  const consoleProblems: { readonly text: string; readonly url: string }[] = [];
  const allowedObserved = new Set<string>();
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleProblems.push({ text: message.text(), url: message.location().url });
    }
  });
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
  page.on('requestfailed', (request) => {
    const failure = request.failure()?.errorText ?? 'unknown';
    const intentionalAbort =
      request.url().endsWith('/agent/run') && failure.includes('ERR_ABORTED');
    if (!intentionalAbort) {
      problems.push(`requestfailed: ${request.method()} ${request.url()} ${failure}`);
    }
  });
  page.on('response', (response) => {
    const url = new URL(response.url());
    const request = response.request();
    const allowed = allowedHttp.some(
      (entry) =>
        entry.method === request.method() &&
        entry.pathname === url.pathname &&
        entry.status === response.status(),
    );
    if (allowed) {
      allowedObserved.add(`${request.method()} ${url.pathname} ${String(response.status())}`);
    }
    if (url.origin === origin && response.status() >= 400 && !allowed) {
      problems.push(`http: ${String(response.status())} ${url.pathname}`);
    }
  });
  return {
    assertClean() {
      const unallowedConsole = consoleProblems.filter((problem) => {
        let pathname = '';
        try {
          pathname = new URL(problem.url).pathname;
        } catch {
          return true;
        }
        return !allowedHttp.some(
          (entry) =>
            entry.pathname === pathname &&
            allowedObserved.has(`${entry.method} ${entry.pathname} ${String(entry.status)}`) &&
            problem.text ===
              'Failed to load resource: the server responded with a status of 409 (Conflict)',
        );
      });
      expect([
        ...problems,
        ...unallowedConsole.map((problem) => `console: ${problem.text}`),
      ]).toEqual([]);
    },
  };
}

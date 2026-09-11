import type { Page } from '@playwright/test';

// live.ts holds what the *-live specs share. They drive an Aura that is already running, through
// the browser, so every API call they make rides the page's own session cookie.

export interface FetchResult {
  readonly status: number;
  readonly text: string;
}

export async function sameOriginFetch(
  page: Page,
  url: string,
  init: { readonly method?: string; readonly body?: string } = {},
): Promise<FetchResult> {
  return page.evaluate(
    async ({ requestURL, requestInit }) => {
      const options: RequestInit = { credentials: 'same-origin' };
      if (requestInit.method !== undefined) options.method = requestInit.method;
      if (requestInit.body !== undefined) {
        options.body = requestInit.body;
        options.headers = { 'Content-Type': 'application/json' };
      }
      const response = await fetch(requestURL, options);
      return { status: response.status, text: await response.text() };
    },
    { requestURL: url, requestInit: init },
  );
}

/** streamFrames decodes an AG-UI SSE body into its JSON frames, skipping anything else. */
export function streamFrames(body: string): readonly Record<string, unknown>[] {
  return body
    .replace(/\r\n/g, '\n')
    .split('\n\n')
    .flatMap((block) => {
      const data = block
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice(5).replace(/^ /, ''))
        .join('\n');
      if (data === '') return [];
      try {
        return [JSON.parse(data) as Record<string, unknown>];
      } catch {
        return [];
      }
    });
}

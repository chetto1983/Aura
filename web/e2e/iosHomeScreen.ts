import type { Page } from '@playwright/test';

export interface SharedFile {
  readonly name: string;
  readonly type: string;
  readonly size: number;
}

/**
 * Runs the page as iOS's home-screen app as far as the cockpit can tell: `navigator.standalone`
 * is true, and the share sheet records what it was handed. Playwright cannot run the real app
 * or the real sheet, so what this proves is the cockpit's side: which control it offers and
 * what it hands over -- not what iOS then does with it (prd.md §3).
 *
 * The sheet keeps the real one's rule: no user activation, no sheet (NotAllowedError). The
 * activation it reads is the browser's own, so a click that outlives it is refused here as it
 * would be on the iPad.
 */
export async function emulateIOSHomeScreen(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const shared: { name: string; type: string; size: number }[] = [];
    Object.defineProperty(window, '__auraShared', { value: shared });
    Object.defineProperty(Navigator.prototype, 'standalone', {
      configurable: true,
      get: () => true,
    });
    Object.defineProperty(Navigator.prototype, 'canShare', {
      configurable: true,
      value: (data: ShareData) => (data.files?.length ?? 0) > 0,
    });
    Object.defineProperty(Navigator.prototype, 'share', {
      configurable: true,
      value: (data: ShareData) => {
        // Optional: an engine without the User Activation API is treated as activated.
        const activation = (navigator as { userActivation?: UserActivation }).userActivation;
        if (activation !== undefined && !activation.isActive) {
          return Promise.reject(new DOMException('no user activation', 'NotAllowedError'));
        }
        for (const file of data.files ?? []) {
          shared.push({ name: file.name, type: file.type, size: file.size });
        }
        return Promise.resolve();
      },
    });
  });
}

export function sharedFiles(page: Page): Promise<SharedFile[]> {
  return page.evaluate(() => [
    ...(window as unknown as { __auraShared: SharedFile[] }).__auraShared,
  ]);
}

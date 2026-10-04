import { vi, type Mock } from 'vitest';

// iOS's home-screen app as far as the cockpit can tell under jsdom: navigator.standalone, a
// share sheet that records what it is handed, and a user activation the test controls.

export interface HomeScreenSheet {
  readonly share: Mock<(data: ShareData) => Promise<void>>;
  /** What navigator.userActivation reports; flip isActive to model a tap that lapsed. */
  activation: { isActive: boolean };
  /** Whether canShare accepts the probe files; refines the default "any File at all". */
  accepts: (files: readonly File[]) => boolean;
}

const STUBBED = ['standalone', 'canShare', 'share', 'userActivation'] as const;

function define(name: string, value: unknown): void {
  Object.defineProperty(navigator, name, { value, configurable: true });
}

export function emulateHomeScreen(): HomeScreenSheet {
  const sheet: HomeScreenSheet = {
    share: vi.fn<(data: ShareData) => Promise<void>>().mockResolvedValue(undefined),
    activation: { isActive: true },
    accepts: (files) => files.length > 0 && files.every((file) => file instanceof File),
  };
  define('standalone', true);
  define('canShare', (data: ShareData) => sheet.accepts(data.files ?? []));
  define('share', sheet.share);
  Object.defineProperty(navigator, 'userActivation', {
    get: () => sheet.activation,
    configurable: true,
  });
  return sheet;
}

export function leaveHomeScreen(): void {
  for (const name of STUBBED) Reflect.deleteProperty(navigator, name);
}

/** The files the n-th share call carried. */
export function sharedFiles(sheet: HomeScreenSheet, call = 0): File[] {
  return sheet.share.mock.calls[call]?.[0].files ?? [];
}

export function mediaQueryList(query: string, matches: boolean): MediaQueryList {
  return {
    matches,
    media: query,
    onchange: null,
    // oxlint-disable-next-line typescript/no-deprecated -- The browser interface requires legacy members in this test double.
    addListener: () => undefined,
    // oxlint-disable-next-line typescript/no-deprecated -- Keep the complete MediaQueryList contract for third-party consumers.
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  };
}

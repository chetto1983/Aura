// Session storage that a browser refusing it (private mode, blocked site data) cannot break: a
// read finds nothing, and a write lasts until the reload.

export function readSessionValue(key: string): string | undefined {
  try {
    return window.sessionStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}

/** Stores value under key; undefined removes the key. */
export function writeSessionValue(key: string, value: string | undefined): void {
  try {
    if (value === undefined) window.sessionStorage.removeItem(key);
    else window.sessionStorage.setItem(key, value);
  } catch {
    // Refused: the value lasts as long as the page that holds it.
  }
}

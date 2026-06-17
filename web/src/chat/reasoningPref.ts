// Persisted reasoning-drawer show/hide preference (D-01). Builder default =
// shown. Kept in its own module so ReasoningDrawer.tsx only exports a component
// (react-refresh/only-export-components).

const PREF_KEY = 'aura.chat.reasoning.shown';

/** Read the persisted preference; builder default is shown (true). */
export function readReasoningPref(): boolean {
  try {
    const raw = localStorage.getItem(PREF_KEY);
    return raw === null ? true : raw === '1';
  } catch {
    return true; // private-mode / disabled storage → default shown
  }
}

/** Persist the preference (best-effort; in-memory state still drives the UI). */
export function writeReasoningPref(shown: boolean): void {
  try {
    localStorage.setItem(PREF_KEY, shown ? '1' : '0');
  } catch {
    // ignore storage errors
  }
}

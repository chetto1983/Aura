// Persisted reasoning-drawer show/hide preference (D-01). The unsaved browser
// default is hidden. Kept in its own module so ReasoningDrawer.tsx only exports
// a component (react-refresh/only-export-components).

const PREF_KEY = 'aura.chat.reasoning.shown';

/** Read the persisted preference; the unsaved browser default is hidden. */
export function readReasoningPref(): boolean {
  try {
    const raw = localStorage.getItem(PREF_KEY);
    return raw === '1';
  } catch {
    return false; // private-mode / disabled storage -> default hidden
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

// The smallest undo/redo that fits the editor: every edit is an immer recipe over the VideoJSON;
// produceWithPatches returns the forward and inverse patches, which ARE the history entry.
// A gesture (drag, trim, slider) opens a transaction: every commit inside it folds into one entry,
// so a 60-event drag undoes in one step.
import { applyPatches, enablePatches, produceWithPatches } from 'immer';

enablePatches();

export function createHistory(initial, { limit = 200 } = {}) {
  let state = initial;
  let past = [];
  let future = [];
  let open = null;
  const listeners = new Set();
  const notify = () => listeners.forEach((fn) => fn(state));

  function commit(label, recipe) {
    const [next, patches, inverse] = produceWithPatches(state, recipe);
    if (!patches.length) return state;
    state = next;
    if (open) {
      open.patches.push(...patches);
      open.inverse.unshift(...inverse);
    } else {
      past = [...past, { label, patches, inverse }].slice(-limit);
    }
    future = [];
    notify();
    return state;
  }

  return {
    get state() { return state; },
    get canUndo() { return past.length > 0 && !open; },
    get canRedo() { return future.length > 0 && !open; },
    get depth() { return { past: past.length, future: future.length }; },
    entries: () => past.map((e) => ({ label: e.label, patches: e.patches.length })),
    serialized: () => JSON.stringify(past),
    commit,
    begin(label) {
      if (open) return;
      open = { label, patches: [], inverse: [] };
    },
    end() {
      if (!open) return;
      if (open.patches.length) past = [...past, open].slice(-limit);
      open = null;
      notify();
    },
    undo() {
      const entry = past.at(-1);
      if (!entry || open) return;
      past = past.slice(0, -1);
      future = [...future, entry];
      state = applyPatches(state, entry.inverse);
      notify();
    },
    redo() {
      const entry = future.at(-1);
      if (!entry || open) return;
      future = future.slice(0, -1);
      past = [...past, entry];
      state = applyPatches(state, entry.patches);
      notify();
    },
    subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); },
  };
}

// The same history with zustand + zundo, for comparison. zundo snapshots whole states; a gesture is
// coalesced by pausing tracking, and on release restoring the pre-gesture state (still paused),
// resuming, and setting the final state — the one set that zundo records.
import { createStore } from 'zustand/vanilla';
import { temporal } from 'zundo';
import { produce } from 'immer';

export function createZundoHistory(initial, { limit = 200 } = {}) {
  const store = createStore(temporal(() => ({ project: initial }), { limit }));
  let before = null;
  return {
    store,
    get state() { return store.getState().project; },
    get depth() { const t = store.temporal.getState(); return { past: t.pastStates.length, future: t.futureStates.length }; },
    commit(_label, recipe) { store.setState({ project: produce(store.getState().project, recipe) }); },
    begin() { before = store.getState().project; store.temporal.getState().pause(); },
    end() {
      const after = store.getState().project;
      store.setState({ project: before });
      store.temporal.getState().resume();
      if (after !== before) store.setState({ project: after });
      before = null;
    },
    undo: () => store.temporal.getState().undo(),
    redo: () => store.temporal.getState().redo(),
    pastStates: () => store.temporal.getState().pastStates,
  };
}

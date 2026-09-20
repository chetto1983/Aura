// history.ts — undo and redo, one step per gesture.
//
// A step is one CALL, not one command: `apply` records the command a button ran, `transaction`
// records whatever a gesture ran between grab and release, so a drag that re-trims on thirty
// pointer events collapses into one undo. A refusal never reaches the stacks — the command threw
// before anything changed, so there is nothing to undo — and a step landing after an undo drops
// the redo branch, because the future it led to no longer exists.

import { applyPatches, castDraft, enablePatches, produceWithPatches, type Patch } from 'immer';
import type { VideoProject } from './project';

// Patches, not snapshots: a project carries every clip and overlay, and an editing session is
// hundreds of steps. `produceWithPatches` gives both directions of each step for the price of
// the step.
enablePatches();

interface Step {
  readonly forward: readonly Patch[];
  readonly back: readonly Patch[];
}

/** Any pure project-to-project function: one command, or a whole gesture's worth of them. */
export type Edit = (project: VideoProject) => VideoProject;

export interface History {
  readonly current: VideoProject;
  readonly canUndo: boolean;
  readonly canRedo: boolean;
  /** Run one command and record it as one step. A refusal propagates and records nothing. */
  apply(edit: Edit): VideoProject;
  /** Run a gesture and record it as ONE step, however many commands it ran. */
  transaction(gesture: Edit): VideoProject;
  undo(): VideoProject;
  redo(): VideoProject;
}

/** How the walk reads a draft, a project or a piece of either: by key, whatever the value is. */
type Indexed = Record<string, unknown>;

/**
 * Whether the walk descends into a value or replaces it whole. Arrays and plain objects are all a
 * project holds — both answer to `Object.keys`, which is why the narrowing covers them together;
 * anything else — a Date, a Map, a class instance someone put in an overlay's props — is written
 * as one value, which is coarser but never wrong.
 */
function walkable(value: unknown): value is Indexed {
  if (Array.isArray(value)) return true;
  if (typeof value !== 'object' || value === null) return false;
  return (value as { constructor?: unknown }).constructor === Object;
}

/**
 * The one name none of this can carry. A draft refuses to define it (`Object.defineProperty()
 * cannot be used on an Immer draft`), assigning it through a draft runs the prototype setter
 * instead of writing a property, and `applyPatches` deep-clones a patch's value by assignment, so
 * the key goes missing on the way back. Measured on immer 11.1.18.
 *
 * An overlay's props take any name, so this is data someone can hold — it is not refused. The walk
 * stops, and `diff` records that step as one replacement of the root, where the project is handed
 * over whole and survives both directions. `__proto__` therefore never appears in a patch's PATH,
 * which is the only way it could have reached `Object.prototype`.
 */
class Unwalkable extends Error {
  constructor() {
    super('videoStudio: a __proto__ property cannot be walked');
  }
}

/** Whether a value written whole hides a `__proto__` anywhere inside it, where a patch would drop it. */
function carriesProto(value: unknown): boolean {
  if (!walkable(value)) return false;
  if (Object.hasOwn(value, '__proto__')) return true;
  return Object.keys(value).some((key) => carriesProto(value[key]));
}

/**
 * Write `next` into the draft, but only where it differs from `base`, so immer records the leaves
 * that changed instead of one replacement of the root — which is what a recipe RETURNING the new
 * project would record, i.e. two whole copies of it per step.
 *
 * The walk is cheap because the commands rebuild by value: an untouched clip, item or lane comes
 * through as the same object, so `Object.is` prunes the subtree in one comparison.
 */
function mergeInto(draft: unknown, base: unknown, next: unknown): void {
  // The three sides are walkable — `mergeKey` proved it before recursing, and the top call is a
  // project against its own draft — so the walk reads them by key.
  const target = draft as Indexed;
  const before = base as Indexed;
  const after = next as Indexed;
  if (Object.hasOwn(before, '__proto__') || Object.hasOwn(after, '__proto__')) {
    throw new Unwalkable();
  }
  for (const key of Object.keys(after)) mergeKey(target, before, after, key);
  if (Array.isArray(next)) {
    // An array that lost its tail shrinks by its length; deleting those indices would leave holes.
    if (before.length !== after.length) target.length = after.length;
    return;
  }
  // `hasOwn`, not `in`: an overlay prop may be named after something Object.prototype already
  // carries, and `'toString' in after` answers yes whether or not the edit dropped it — which
  // would leave the stale property in the draft, record no patch, and lose the edit in silence.
  for (const key of Object.keys(before)) {
    if (!Object.hasOwn(after, key)) Reflect.deleteProperty(target, key);
  }
}

function mergeKey(target: Indexed, base: Indexed, next: Indexed, key: string): void {
  const before = base[key];
  const after = next[key];
  if (Object.is(before, after)) return;
  if (walkable(before) && walkable(after) && Array.isArray(before) === Array.isArray(after)) {
    mergeInto(target[key], before, after);
    return;
  }
  // Written whole, so the deep look: a patch carries this value, and `applyPatches` would rebuild
  // it key by key on the way back.
  if (carriesProto(before) || carriesProto(after)) throw new Unwalkable();
  target[key] = after;
}

/**
 * The step from `base` to `next`, both ways, and the project to keep. Normally the leaves that
 * changed; for the one shape the walk cannot enter, a single replacement of the root — coarser,
 * but it carries what a leaf patch cannot, and it keeps a valid project editable.
 */
function diff(base: VideoProject, next: VideoProject): readonly [VideoProject, Patch[], Patch[]] {
  try {
    return produceWithPatches(base, (draft) => {
      mergeInto(draft, base, next);
    });
  } catch (error) {
    if (!(error instanceof Unwalkable)) throw error;
    return produceWithPatches(base, () => castDraft(next));
  }
}

export function createHistory(initial: VideoProject): History {
  let current = initial;
  const done: Step[] = [];
  const undone: Step[] = [];

  function record(edit: Edit): VideoProject {
    const base = current;
    // The edit runs on the project itself rather than on a draft: the commands are pure, and a
    // refusal has to throw here, before a step exists to push.
    const next = edit(base);
    const [merged, forward, back] = diff(base, next);
    if (forward.length === 0) return current;
    done.push({ forward, back });
    undone.length = 0;
    current = merged;
    return current;
  }

  /** Walk one step off `from` onto `to`, in the direction the caller asks that step for. */
  function replay(
    from: Step[],
    to: Step[],
    direction: (step: Step) => readonly Patch[],
  ): VideoProject {
    const step = from.pop();
    if (step === undefined) return current;
    to.push(step);
    current = applyPatches(current, direction(step));
    return current;
  }

  return {
    get current() {
      return current;
    },
    get canUndo() {
      return done.length > 0;
    },
    get canRedo() {
      return undone.length > 0;
    },
    apply: record,
    // The same operation under the name each caller uses: a button applies a command, a drag
    // commits a gesture on release. One call is one step either way.
    transaction: record,
    undo: () => replay(done, undone, (step) => step.back),
    redo: () => replay(undone, done, (step) => step.forward),
  };
}

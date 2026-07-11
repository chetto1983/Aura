import { useState } from 'react';

export interface ReasoningEffortSeam {
  readonly effort: string;
  readonly setEffort: (effort: string) => void;
}

// useReasoningEffort owns the per-conversation reasoning-effort selection. Unlike usePinnedSkill
// (a per-turn pin CLEARED after send), the effort is a CONVERSATION preference: it hydrates from
// the conversation DTO's persisted `ReasoningEffort` (37E-03) on thread open, restores on reopen,
// and is NEVER cleared on send. A stored/selected level the active model does not advertise clamps
// back to 'auto' (D-13 defense-in-depth; 'auto' is always advertised). Lifted into a hook so
// ExternalStoreChat stays under the 600-LOC cap (all its state lives here, not inline).
//
// Both transitions adjust state DURING render (the sanctioned "reset state on a prop change"
// pattern — the same one Composer.tsx uses for its picker's activeIndexFilter), not in an effect:
// setState-in-effect triggers cascading renders and is lint-forbidden here.
export function useReasoningEffort(
  threadId: string,
  hydratedEffort: string | undefined,
  levels: readonly string[],
): ReasoningEffortSeam {
  const [effort, setEffort] = useState('auto');
  const [hydratedKey, setHydratedKey] = useState<string | null>(null);

  // Hydrate once per thread. Keyed on threadId so a manual pick is NOT clobbered by a later
  // refetch of the SAME thread (the post-send conversation invalidation), and re-arms when a
  // different thread opens. For a non-empty thread we wait for the conversation query to resolve
  // (hydratedEffort defined) before locking in; an empty/absent stored value hydrates as 'auto'
  // — the adaptive default runs unchanged (D-07).
  if (hydratedKey !== threadId && (threadId.length === 0 || hydratedEffort !== undefined)) {
    setHydratedKey(threadId);
    const next = hydratedEffort !== undefined && hydratedEffort.length > 0 ? hydratedEffort : 'auto';
    if (next !== effort) setEffort(next);
  }

  // Clamp a value the active model does not advertise back to 'auto' once the advertised levels
  // arrive (they load asynchronously after mount). 'auto' is always in the set, so the clamp is a
  // safe floor and converges in one extra render — it never leaves the selector unrenderable.
  if (levels.length > 0 && !levels.includes(effort)) {
    setEffort('auto');
  }

  return { effort, setEffort };
}

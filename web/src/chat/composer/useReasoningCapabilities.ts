import { useEffect, useState } from 'react';
import {
  fetchReasoningCapabilities,
  REASONING_CAPABILITIES_FLOOR,
  type ReasoningCapabilities,
} from './api';

export interface ReasoningCapabilitiesState extends ReasoningCapabilities {
  /** False only while the capability request is still in flight. */
  readonly settled: boolean;
}

// useReasoningCapabilities fetches the active model's advertised effort levels once on mount for
// the Composer selector (D-13 — render ONLY what the model advertises, never a placebo).
// fetchReasoningCapabilities already degrades to the {auto,off} floor on any throw (the D-09 no-op
// source); the mounted guard keeps a late rejection or an unmount from stranding the caller. It
// mirrors useComposerSkills exactly. Lifted into a hook so ExternalStoreChat stays ≤600 LOC.
export function useReasoningCapabilities(): ReasoningCapabilitiesState {
  const [capabilities, setCapabilities] = useState<ReasoningCapabilities>(
    REASONING_CAPABILITIES_FLOOR,
  );
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    let active = true;
    void fetchReasoningCapabilities()
      .then((caps) => {
        if (active) {
          setCapabilities(caps);
          setSettled(true);
        }
      })
      .catch(() => {
        if (active) {
          setCapabilities(REASONING_CAPABILITIES_FLOOR);
          setSettled(true);
        }
      });
    return () => {
      active = false;
    };
  }, []);
  return { ...capabilities, settled };
}

import { useQuery } from '@tanstack/react-query';
import {
  fetchReasoningCapabilities,
  REASONING_CAPABILITIES_FLOOR,
  type ReasoningCapabilities,
} from './api';

export interface ReasoningCapabilitiesState extends ReasoningCapabilities {
  /** False only while the capability request is still in flight. */
  readonly settled: boolean;
}

export const REASONING_CAPABILITIES_QUERY_KEY = ['composer', 'reasoning-capabilities'] as const;

// useReasoningCapabilities caches the active model's advertised effort levels for
// the Composer selector (D-13 — render ONLY what the model advertises, never a placebo).
// fetchReasoningCapabilities already degrades to the {auto,off} floor on any throw (the D-09 no-op
// source); the mounted guard keeps a late rejection or an unmount from stranding the caller. It
// mirrors useComposerSkills exactly. Lifted into a hook so ExternalStoreChat stays ≤600 LOC.
export function useReasoningCapabilities(): ReasoningCapabilitiesState {
  const query = useQuery({
    queryKey: REASONING_CAPABILITIES_QUERY_KEY,
    queryFn: fetchReasoningCapabilities,
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  });
  return {
    ...(query.data ?? REASONING_CAPABILITIES_FLOOR),
    settled: !query.isLoading,
  };
}

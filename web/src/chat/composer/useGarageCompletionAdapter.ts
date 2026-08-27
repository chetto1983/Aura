import { useEffect, useMemo, useRef, useState } from 'react';
import type { Unstable_TriggerAdapter } from '@assistant-ui/core';
import type { Unstable_TriggerItem } from '@assistant-ui/react';

interface CompletionResult {
  readonly query: string | null;
  readonly ordinal: number;
  readonly epoch: number;
  readonly items: readonly Unstable_TriggerItem[];
}

interface CompletionRequest {
  readonly query: string;
  readonly ordinal: number;
  readonly epoch: number;
}

const EMPTY_RESULT: CompletionResult = { query: null, ordinal: 0, epoch: 0, items: [] };

/**
 * Async data bridge for assistant-ui's synchronous TriggerAdapter contract. The primitive
 * still owns trigger detection, navigation and ARIA; this hook only debounces one Garage
 * page read and drops stale responses. It replaces the package's explicitly deprecated
 * live-completion hook while that API remains experimental.
 */
export function useGarageCompletionAdapter(
  fetcher: (query: string) => Promise<readonly Unstable_TriggerItem[]>,
  enabled: boolean,
  debounceMs = 60,
): { readonly adapter: Unstable_TriggerAdapter; readonly isLoading: boolean } {
  const [request, setRequest] = useState<CompletionRequest | null>(null);
  const [result, setResult] = useState<CompletionResult>(EMPTY_RESULT);
  const pendingQuery = useRef<string | null>(null);
  const requestGeneration = useRef(0);
  const requestOrdinal = useRef(0);
  const cacheEpoch = useRef(0);

  useEffect(() => {
    if (enabled) return;
    requestGeneration.current += 1;
    cacheEpoch.current += 1;
    pendingQuery.current = null;
  }, [enabled]);

  useEffect(() => {
    if (!enabled || request === null) return;
    const generation = ++requestGeneration.current;
    const timer = window.setTimeout(() => {
      void fetcher(request.query).then(
        (items) => {
          if (generation !== requestGeneration.current) return;
          setResult({ ...request, items });
        },
        () => {
          if (generation !== requestGeneration.current) return;
          setResult({ ...request, items: [] });
        },
      );
    }, debounceMs);
    return () => {
      window.clearTimeout(timer);
      requestGeneration.current += 1;
    };
  }, [debounceMs, enabled, fetcher, request]);

  const adapter = useMemo<Unstable_TriggerAdapter>(
    () => ({
      categories: () => [],
      categoryItems: () => [],
      search: (query) => {
        if (!enabled) return [];
        const cacheIsCurrent = result.epoch === cacheEpoch.current;
        if (cacheIsCurrent && result.query === query) {
          if (request !== null && request.query !== query) {
            pendingQuery.current = query;
            queueMicrotask(() => {
              if (pendingQuery.current !== query) return;
              requestGeneration.current += 1;
              setRequest(null);
            });
          }
          return result.items;
        }
        if (pendingQuery.current !== query) {
          pendingQuery.current = query;
          // TriggerAdapter.search is synchronous and is called while the primitive renders.
          // Schedule the state hand-off rather than updating React during that render.
          queueMicrotask(() => {
            if (pendingQuery.current !== query) return;
            setRequest({
              query,
              ordinal: ++requestOrdinal.current,
              epoch: cacheEpoch.current,
            });
          });
        }
        return [];
      },
    }),
    [enabled, request, result],
  );

  const isLoading =
    enabled &&
    request !== null &&
    (result.epoch !== request.epoch || result.ordinal !== request.ordinal);
  return { adapter, isLoading };
}

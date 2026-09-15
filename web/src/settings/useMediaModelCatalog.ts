import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchMediaModels, type MediaCatalogModel, type MediaKind } from './mediaModelCatalog';
import { catalogError, type ModelCatalogState } from './useModelCatalog';

interface CatalogRequest {
  readonly nonce: number;
  readonly refresh: boolean;
}

interface CatalogAnswer {
  readonly kind: MediaKind;
  readonly request: CatalogRequest;
  readonly models: readonly MediaCatalogModel[];
  readonly error: string | undefined;
}

const FIRST_REQUEST: CatalogRequest = { nonce: 0, refresh: false };

// useMediaModelCatalog keeps the image or video catalogue of the SAVED route. There is no form
// URL to follow and nothing to debounce: the daemon answers from the route it runs on, so the
// list is only asked for while the Cloud route is showing the media rows.
//
// State is set only once an answer arrives; loading and idle are derived from whether the
// latest answer belongs to the current request. Each request's AbortController is its
// sequence ticket: disabling, unmounting or refreshing aborts it, and an answer that arrives
// on an aborted signal (a server that ignored the abort) is dropped.
export function useMediaModelCatalog(
  kind: MediaKind,
  enabled: boolean,
): ModelCatalogState<MediaCatalogModel> {
  const [request, setRequest] = useState<CatalogRequest>(FIRST_REQUEST);
  const [answer, setAnswer] = useState<CatalogAnswer | undefined>(undefined);
  // A refresh bypasses the daemon cache once: re-enabling the rows later reads the cache again.
  const refreshed = useRef(FIRST_REQUEST.nonce);

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const refresh = request.refresh && refreshed.current !== request.nonce;
    refreshed.current = request.nonce;
    async function run() {
      try {
        const models = await fetchMediaModels(kind, refresh, controller.signal);
        if (controller.signal.aborted) return;
        setAnswer({ kind, request, models, error: undefined });
      } catch (err) {
        if (controller.signal.aborted) return;
        setAnswer({ kind, request, models: [], error: catalogError(err) });
      }
    }
    void run();
    return () => {
      controller.abort();
    };
  }, [enabled, kind, request]);

  const reload = useCallback(() => {
    setRequest((previous) => ({ nonce: previous.nonce + 1, refresh: true }));
  }, []);

  if (!enabled) return { models: [], status: 'idle', error: undefined, reload };
  const sameKind = answer?.kind === kind;
  if (!sameKind || answer.request !== request) {
    return { models: sameKind ? answer.models : [], status: 'loading', error: undefined, reload };
  }
  return {
    models: answer.models,
    status: answer.error === undefined ? 'ready' : 'error',
    error: answer.error,
    reload,
  };
}

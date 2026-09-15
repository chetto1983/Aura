import { useCallback, useEffect, useState } from 'react';
import { fetchMediaModels, type MediaCatalogModel, type MediaKind } from './mediaModelCatalog';
import { catalogError, type ModelCatalogState } from './useModelCatalog';

interface CatalogRequest {
  readonly kind: MediaKind;
  readonly enabled: boolean;
  readonly route: string;
  readonly nonce: number;
  readonly refresh: boolean;
}

interface CatalogAnswer {
  readonly request: CatalogRequest;
  readonly models: readonly MediaCatalogModel[];
  readonly error: string | undefined;
}

// useMediaModelCatalog keeps the image or video catalogue of the SAVED route. The daemon
// answers from the route it runs on, so `route` names that saved route: saving a different one
// asks again, and `enabled` is true only while the rows show on a saved Cloud route.
//
// Every change of kind, route or `enabled`, and every Refresh, is a new request, adjusted while
// rendering so the render that changed already reads as loading rather than as the previous
// answer. State is otherwise set only when an answer arrives. Each request's AbortController
// is its sequence ticket: a superseded request is aborted, and an answer that still arrives on
// an aborted signal (a server that ignored the abort) is dropped.
export function useMediaModelCatalog(
  kind: MediaKind,
  enabled: boolean,
  route: string,
): ModelCatalogState<MediaCatalogModel> {
  const [request, setRequest] = useState<CatalogRequest>({
    kind,
    enabled,
    route,
    nonce: 0,
    refresh: false,
  });
  const [answer, setAnswer] = useState<CatalogAnswer | undefined>(undefined);
  if (request.kind !== kind || request.enabled !== enabled || request.route !== route) {
    setRequest({ kind, enabled, route, nonce: request.nonce + 1, refresh: false });
  }

  useEffect(() => {
    if (!request.enabled) return;
    const controller = new AbortController();
    async function run() {
      try {
        const models = await fetchMediaModels(request.kind, request.refresh, controller.signal);
        if (controller.signal.aborted) return;
        setAnswer({ request, models, error: undefined });
      } catch (err) {
        if (controller.signal.aborted) return;
        setAnswer({ request, models: [], error: catalogError(err) });
      }
    }
    void run();
    return () => {
      controller.abort();
    };
  }, [request]);

  const reload = useCallback(() => {
    setRequest((previous) => ({ ...previous, nonce: previous.nonce + 1, refresh: true }));
  }, []);

  if (!enabled) return { models: [], status: 'idle', error: undefined, reload };
  if (answer?.request !== request) {
    // A refresh of the same list keeps it on screen; another kind or route never shows it.
    const sameList = answer?.request.kind === kind && answer.request.route === route;
    return { models: sameList ? answer.models : [], status: 'loading', error: undefined, reload };
  }
  return {
    models: answer.models,
    status: answer.error === undefined ? 'ready' : 'error',
    error: answer.error,
    reload,
  };
}

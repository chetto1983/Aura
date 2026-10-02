import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { fetchLLMModels, type LLMCatalogModel } from './settingsApi';

export type CatalogStatus = 'idle' | 'loading' | 'ready' | 'error';

export interface ModelCatalogState<M = LLMCatalogModel> {
  readonly models: readonly M[];
  readonly status: CatalogStatus;
  readonly error: string | undefined;
  readonly reload: () => void;
}

/** Why a catalogue read failed, as the picker shows it. */
export function catalogError(err: unknown): string {
  return err instanceof Error && err.message.trim() !== '' ? err.message : String(err);
}

// The probe is debounced because the base URL is a text box: fetching on every keystroke
// would hammer the endpoint with URLs nobody finished typing.
const PROBE_DEBOUNCE_MS = 600;

// probeable rejects a half-typed URL before it costs a round trip and a 400.
function probeable(provider: string, baseURL: string): boolean {
  if (provider.trim() === '') return false;
  try {
    const parsed = new URL(baseURL.trim());
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
}

// useModelCatalog keeps the list of models the CURRENT form route publishes. It follows
// the form rather than the saved settings, so the operator sees what an endpoint serves
// before committing to it.
export function useModelCatalog(
  provider: string,
  baseURL: string,
  enabled = true,
): ModelCatalogState {
  const route = useMemo(() => ({ provider, baseURL, enabled }), [baseURL, enabled, provider]);
  const [catalog, setCatalog] = useState<{
    readonly route: typeof route;
    readonly models: readonly LLMCatalogModel[];
    readonly status: CatalogStatus;
    readonly error: string | undefined;
  }>();
  // Every probe carries a sequence number: a slow answer for a route the operator has
  // already moved off must not overwrite the list for the route they are looking at.
  const sequence = useRef(0);
  const controller = useRef<AbortController | null>(null);

  const load = useCallback(async () => {
    controller.current?.abort();
    sequence.current += 1;
    const ticket = sequence.current;
    if (!route.enabled || !probeable(route.provider, route.baseURL)) {
      setCatalog({ route, models: [], status: 'idle', error: undefined });
      return;
    }
    const request = new AbortController();
    controller.current = request;
    setCatalog({ route, models: [], status: 'loading', error: undefined });
    try {
      const list = await fetchLLMModels(
        route.provider.trim(),
        route.baseURL.trim(),
        request.signal,
      );
      if (ticket !== sequence.current) return;
      setCatalog({ route, models: list, status: 'ready', error: undefined });
    } catch (err) {
      if (ticket !== sequence.current) return;
      setCatalog({ route, models: [], status: 'error', error: catalogError(err) });
    }
  }, [route]);

  useEffect(() => {
    const timer =
      route.enabled && probeable(route.provider, route.baseURL)
        ? setTimeout(() => {
            void load();
          }, PROBE_DEBOUNCE_MS)
        : undefined;
    return () => {
      if (timer !== undefined) clearTimeout(timer);
      sequence.current += 1;
      controller.current?.abort();
    };
  }, [load, route]);

  // The refresh button skips the debounce: the operator asking for the list now is not a
  // keystroke to wait out.
  const reload = useCallback(() => {
    void load();
  }, [load]);

  if (enabled && catalog?.route === route) return { ...catalog, reload };
  return {
    models: [],
    status: enabled && probeable(provider, baseURL) ? 'loading' : 'idle',
    error: undefined,
    reload,
  };
}

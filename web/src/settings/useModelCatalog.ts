import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchLLMModels, type LLMCatalogModel } from './settingsApi';

export type CatalogStatus = 'idle' | 'loading' | 'ready' | 'error';

export interface ModelCatalogState {
  readonly models: readonly LLMCatalogModel[];
  readonly status: CatalogStatus;
  readonly error: string | undefined;
  readonly reload: () => void;
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
export function useModelCatalog(provider: string, baseURL: string): ModelCatalogState {
  const [models, setModels] = useState<readonly LLMCatalogModel[]>([]);
  const [status, setStatus] = useState<CatalogStatus>('idle');
  const [error, setError] = useState<string | undefined>(undefined);
  // Every probe carries a sequence number: a slow answer for a route the operator has
  // already moved off must not overwrite the list for the route they are looking at.
  const sequence = useRef(0);

  const load = useCallback(async () => {
    if (!probeable(provider, baseURL)) {
      setModels([]);
      setStatus('idle');
      setError(undefined);
      return;
    }
    sequence.current += 1;
    const ticket = sequence.current;
    setStatus('loading');
    setError(undefined);
    try {
      const list = await fetchLLMModels(provider.trim(), baseURL.trim());
      if (ticket !== sequence.current) return;
      setModels(list);
      setStatus('ready');
    } catch (err) {
      if (ticket !== sequence.current) return;
      setModels([]);
      setStatus('error');
      setError(err instanceof Error && err.message.trim() !== '' ? err.message : String(err));
    }
  }, [baseURL, provider]);

  useEffect(() => {
    const timer = setTimeout(() => {
      void load();
    }, PROBE_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
    };
  }, [load]);

  // The refresh button skips the debounce: the operator asking for the list now is not a
  // keystroke to wait out.
  const reload = useCallback(() => {
    void load();
  }, [load]);

  return { models, status, error, reload };
}

import { useEffect, useState, useRef } from 'react';
import { ApiError } from '@/api';

export interface ApiState<T> {
  data?: T;
  error?: ApiError | Error;
  loading: boolean;
  /** True only when a previous successful fetch is being shown despite a recent failure. */
  stale: boolean;
}

/**
 * useApi — shared fetch + optional polling.
 *
 * - First fetch error: data undefined, error set, loading false.
 * - Subsequent fetch error: keep previous data, set error, mark stale.
 *   UIs should show a "⚠ stale" pill rather than blanking the panel.
 * - Tab visibility: poll pauses when document.visibilityState === "hidden".
 * - The fetcher must be referentially stable across renders (use useCallback
 *   when the fetcher depends on props/state).
 */
export function useApi<T>(
  fetcher: () => Promise<T>,
  intervalMs?: number,
): ApiState<T> & { refetch: () => void } {
  const [state, setState] = useState<ApiState<T>>({ loading: true, stale: false });
  const tickRef = useRef<() => void>(() => {});

  useEffect(() => {
    let alive = true;
    let timerId: ReturnType<typeof setInterval> | null = null;

    const tick = async () => {
      try {
        const data = await fetcher();
        if (!alive) return;
        setState({ data, loading: false, stale: false });
      } catch (err) {
        if (!alive) return;
        const e = err as Error;
        setState((prev) => ({
          data: prev.data,
          error: e,
          loading: false,
          stale: prev.data !== undefined,
        }));
      }
    };
    tickRef.current = tick;

    void tick();

    if (intervalMs) {
      const startInterval = () => {
        if (timerId) return;
        timerId = setInterval(() => {
          if (document.visibilityState === 'visible') void tick();
        }, intervalMs);
      };
      const stopInterval = () => {
        if (!timerId) return;
        clearInterval(timerId);
        timerId = null;
      };
      const onVisibility = () => {
        if (document.visibilityState === 'visible') {
          void tick();
          startInterval();
        } else {
          stopInterval();
        }
      };
      startInterval();
      document.addEventListener('visibilitychange', onVisibility);
      return () => {
        alive = false;
        stopInterval();
        document.removeEventListener('visibilitychange', onVisibility);
      };
    }

    return () => {
      alive = false;
    };
  }, [fetcher, intervalMs]);

  return { ...state, refetch: () => tickRef.current() };
}

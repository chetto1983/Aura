import { useEffect, useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  UPDATE_QUERY_KEY,
  applyUpdate,
  deferUpdate,
  fetchSystemUpdate,
  isUpdating,
  updatePollInterval,
  type SystemUpdateStatus,
} from './systemUpdateApi';

export interface SystemUpdateView {
  /** undefined until a managed appliance has answered; a dev stack stays undefined. */
  readonly status: SystemUpdateStatus | undefined;
  /** The daemon is restarting into the new build: applying, or unreachable after a request. */
  readonly restarting: boolean;
  /** When the status was last read; the clock the dialog measures deadlines against. */
  readonly now: number;
}

function reloadPage(): void {
  window.location.reload();
}

export function useSystemUpdate(reload: () => void = reloadPage): SystemUpdateView {
  const query = useQuery({
    queryKey: UPDATE_QUERY_KEY,
    queryFn: ({ signal }) => fetchSystemUpdate(signal),
    retry: false,
    refetchInterval: (current) => updatePollInterval(current.state.data),
  });
  const status = query.data?.managed === true ? query.data : undefined;
  const firstRev = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (status === undefined) return;
    if (firstRev.current === undefined) {
      firstRev.current = status.running_rev;
      return;
    }
    // A new process with a new build is serving: this page still runs the old bundle.
    if (status.state === 'current' && status.running_rev !== firstRev.current) reload();
  }, [status, reload]);

  // While the daemon restarts its answers stop, and the cache keeps the last one it gave.
  const unreachable = query.isError && isUpdating(status?.state);
  return {
    status,
    restarting: status?.state === 'applying' || unreachable,
    now: query.dataUpdatedAt,
  };
}

// The answer is written into the cache at once so the page reacts before the next poll; the
// refetch that follows is not awaited, because after an apply the daemon may already be gone.
function useUpdateCache() {
  const client = useQueryClient();
  return {
    patch: (fields: Partial<SystemUpdateStatus>) => {
      client.setQueryData<SystemUpdateStatus>(UPDATE_QUERY_KEY, (prev) =>
        prev === undefined ? prev : { ...prev, ...fields },
      );
    },
    refresh: () => {
      void client.invalidateQueries({ queryKey: UPDATE_QUERY_KEY });
    },
  };
}

export function useApplyUpdate() {
  const cache = useUpdateCache();
  return useMutation({
    mutationFn: applyUpdate,
    onSuccess: (res) => {
      cache.patch({ state: res.state });
    },
    onSettled: cache.refresh,
  });
}

export function useDeferUpdate() {
  const cache = useUpdateCache();
  return useMutation({
    mutationFn: deferUpdate,
    onSuccess: (res) => {
      cache.patch({ deferred_until: res.deferred_until });
    },
    onSettled: cache.refresh,
  });
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  acceptExternalRemoteAccess,
  configureRemoteAccess,
  deleteRemoteAccess,
  disableRemoteAccess,
  fetchRemoteAccess,
  reconcileRemoteAccess,
  refreshRemoteAccessToken,
  verifyRemoteAccessToken,
  type RemoteAccessConfiguration,
  type RemoteAccessPhase,
} from './remoteAccessApi';

export const REMOTE_ACCESS_KEY = ['settings', 'remote-access'] as const;

const transientPhases: ReadonlySet<RemoteAccessPhase> = new Set([
  'validating',
  'waiting_nameservers',
  'provisioning',
  'connecting',
  'deleting',
]);

export function remoteAccessPollInterval(phase: RemoteAccessPhase | undefined): number | false {
  if (phase === undefined || phase === 'disabled' || phase === 'error') return false;
  if (transientPhases.has(phase)) return 2_000;
  return 30_000;
}

export function useRemoteAccess() {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: REMOTE_ACCESS_KEY,
    queryFn: ({ signal }) => fetchRemoteAccess(signal),
    retry: false,
    refetchInterval: (state) => remoteAccessPollInterval(state.state.data?.phase),
  });
  const invalidate = () => client.invalidateQueries({ queryKey: REMOTE_ACCESS_KEY });
  const configure = useMutation({ mutationFn: configureRemoteAccess, onSettled: invalidate });
  const verifyToken = useMutation({ mutationFn: verifyRemoteAccessToken });
  const reconcile = useMutation({ mutationFn: reconcileRemoteAccess, onSettled: invalidate });
  const refreshToken = useMutation({ mutationFn: refreshRemoteAccessToken, onSettled: invalidate });
  const disable = useMutation({ mutationFn: disableRemoteAccess, onSettled: invalidate });
  const remove = useMutation({ mutationFn: deleteRemoteAccess, onSettled: invalidate });
  const acceptExternal = useMutation({
    mutationFn: acceptExternalRemoteAccess,
    onSettled: invalidate,
  });
  return {
    query,
    configure,
    verifyToken,
    reconcile,
    refreshToken,
    disable,
    remove,
    acceptExternal,
  };
}

export type { RemoteAccessConfiguration };

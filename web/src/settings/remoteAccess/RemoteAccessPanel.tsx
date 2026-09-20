import { useTranslation } from 'react-i18next';
import { RemoteAccessStatus, type RemoteAccessAction } from './RemoteAccessStatus';
import { RemoteAccessWizard } from './RemoteAccessWizard';
import {
  configureRemoteAccess,
  verifyRemoteAccessToken,
  type RemoteAccessConfiguration,
  type RemoteAccessStatusDTO,
} from './remoteAccessApi';
import { useRemoteAccess } from './useRemoteAccess';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

export function RemoteAccessPanel() {
  const { t } = useTranslation();
  const remote = useRemoteAccess();
  const status = remote.query.data;
  if (remote.query.isLoading)
    return (
      <section role="status" className="grid min-h-28 place-items-center text-sm text-text-muted">
        {t('remoteAccess.loading')}
      </section>
    );
  if (remote.query.isError || status === undefined)
    return (
      <Alert variant="destructive">
        <AlertDescription>
          {t('remoteAccess.loadError')}
          <Button
            type="button"
            variant="outline"
            className="mt-3"
            onClick={() => void remote.query.refetch()}
          >
            {t('remoteAccess.retry')}
          </Button>
        </AlertDescription>
      </Alert>
    );
  const configured = isManaged(status);
  const refresh = async () => {
    await remote.query.refetch();
  };
  const action = async (name: RemoteAccessAction) => {
    if (name === 'reconcile') await remote.reconcile.mutateAsync(status.generation);
    if (name === 'refresh') await remote.refreshToken.mutateAsync(status.generation);
    if (name === 'disable') await remote.disable.mutateAsync();
    await refresh();
  };
  const replace = async (token: string) => {
    const accounts = await verifyRemoteAccessToken(token);
    const account = accounts.find((item) => item.id === status.account_id);
    if (!account || !status.zone_name) throw new Error('replacement unavailable');
    await configureRemoteAccess({
      enabled: true,
      generation: status.generation,
      account_id: account.id,
      zone_name: status.zone_name,
      public_label: status.public_hostname?.split('.')[0] ?? 'aura',
      warp_label: status.warp_hostname?.split('.')[0] ?? 'aura-warp',
      api_token: token,
    });
    await refresh();
  };
  return configured ? (
    <RemoteAccessStatus
      status={status}
      onAction={action}
      onDelete={async (hostname) => {
        await remote.remove.mutateAsync(hostname);
        await refresh();
      }}
      onReplaceToken={replace}
      pending={
        remote.reconcile.isPending ||
        remote.refreshToken.isPending ||
        remote.disable.isPending ||
        remote.remove.isPending
      }
    />
  ) : (
    <RemoteAccessWizard
      status={status}
      onVerifyToken={verifyRemoteAccessToken}
      onConfigure={async (configuration: RemoteAccessConfiguration) => {
        await configureRemoteAccess(configuration);
        await refresh();
      }}
      accepting={remote.acceptExternal.isPending}
      onAcceptExternal={async () => {
        await remote.acceptExternal.mutateAsync(status.generation);
        await refresh();
      }}
    />
  );
}

function isManaged(status: RemoteAccessStatusDTO): boolean {
  return (
    status.phase === 'healthy' ||
    status.phase === 'degraded' ||
    status.phase === 'error' ||
    status.phase === 'deleting' ||
    (status.phase === 'disabled' &&
      (status.public_hostname !== undefined || status.warp_hostname !== undefined))
  );
}

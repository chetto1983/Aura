import { useTranslation } from 'react-i18next';
import { RemoteAccessStatus, type RemoteAccessAction } from './RemoteAccessStatus';
import { RemoteAccessWizard } from './RemoteAccessWizard';
import type { RemoteAccessConfiguration } from './remoteAccessApi';
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
  const configured = ['healthy', 'degraded', 'error', 'deleting'].includes(status.phase);
  const action = (name: RemoteAccessAction) => {
    if (name === 'reconcile') remote.reconcile.mutate(status.generation);
    if (name === 'refresh') remote.refreshToken.mutate(status.generation);
    if (name === 'disable') remote.disable.mutate();
  };
  const replace = async (token: string) => {
    const accounts = await remote.verifyToken.mutateAsync(token);
    const account = accounts.find((item) => item.id === status.account_id);
    if (!account || !status.zone_name) throw new Error('replacement unavailable');
    await remote.configure.mutateAsync({
      enabled: true,
      generation: status.generation,
      account_id: account.id,
      zone_name: status.zone_name,
      public_label: status.public_hostname?.split('.')[0] ?? 'aura',
      warp_label: status.warp_hostname?.split('.')[0] ?? 'aura-warp',
      api_token: token,
    });
  };
  return configured ? (
    <RemoteAccessStatus
      status={status}
      onAction={action}
      onDelete={(hostname) => remote.remove.mutateAsync(hostname)}
      onReplaceToken={replace}
      pending={
        remote.reconcile.isPending ||
        remote.refreshToken.isPending ||
        remote.disable.isPending ||
        remote.remove.isPending ||
        remote.configure.isPending
      }
    />
  ) : (
    <RemoteAccessWizard
      status={status}
      onVerifyToken={(token) => remote.verifyToken.mutateAsync(token)}
      onConfigure={(configuration: RemoteAccessConfiguration) =>
        remote.configure.mutateAsync(configuration)
      }
      accepting={remote.acceptExternal.isPending}
      onAcceptExternal={() => {
        remote.acceptExternal.mutate(status.generation);
      }}
    />
  );
}

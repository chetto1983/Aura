import { useTranslation } from 'react-i18next';
import { RemoteAccessWizard } from './RemoteAccessWizard';
import { configureRemoteAccess, verifyRemoteAccessToken } from './remoteAccessApi';
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
  return (
    <RemoteAccessWizard
      status={status}
      actions={{
        verify: verifyRemoteAccessToken,
        configure: async (configuration) => {
          // A refused save refreshes too: a phase that does not poll would otherwise answer every
          // retry with the same stale generation.
          try {
            await configureRemoteAccess(configuration);
          } finally {
            await remote.query.refetch();
          }
        },
        dismissAbandoned: () => remote.dismiss.mutateAsync(),
        reconcile: () => remote.reconcile.mutateAsync(status.generation),
        refreshToken: () => remote.refreshToken.mutateAsync(status.generation),
        disable: () => remote.disable.mutateAsync(),
        remove: (hostname) => remote.remove.mutateAsync(hostname),
        acceptExternal: () => remote.acceptExternal.mutateAsync(status.generation),
      }}
    />
  );
}

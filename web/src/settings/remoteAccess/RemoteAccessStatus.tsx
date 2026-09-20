import { useRef, useState } from 'react';
import { ExternalLink, RefreshCw, RotateCw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import type { RemoteAccessStatusDTO } from './remoteAccessApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

export type RemoteAccessAction = 'reconcile' | 'refresh' | 'disable';

interface RemoteAccessStatusProps {
  readonly status: RemoteAccessStatusDTO;
  readonly onAction: (action: RemoteAccessAction) => void;
  readonly onDelete: (hostname: string) => Promise<void>;
  readonly onReplaceToken?: (token: string) => Promise<void>;
  readonly pending?: boolean;
}

export function RemoteAccessStatus({
  status,
  onAction,
  onDelete,
  onReplaceToken,
  pending = false,
}: RemoteAccessStatusProps) {
  const { t } = useTranslation();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const [token, setToken] = useState('');
  const [replacementFailed, setReplacementFailed] = useState(false);
  const [replacing, setReplacing] = useState(false);
  const deleteTrigger = useRef<HTMLButtonElement>(null);
  const hostname = status.public_hostname ?? '';
  const terminal = status.phase === 'error' || status.phase === 'degraded';

  function closeDelete(open: boolean) {
    setDeleteOpen(open);
    if (!open) {
      setConfirmation('');
      requestAnimationFrame(() => deleteTrigger.current?.focus());
    }
  }

  async function replaceToken() {
    if (!onReplaceToken || token.trim() === '' || replacing) return;
    setReplacing(true);
    setReplacementFailed(false);
    try {
      await onReplaceToken(token);
      setToken('');
    } catch {
      setReplacementFailed(true);
    } finally {
      setReplacing(false);
    }
  }

  return (
    <section aria-labelledby="remote-access-heading" className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h2 id="remote-access-heading" className="text-[20px] font-semibold text-text">
          {t('remoteAccess.status.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('remoteAccess.body')}
        </p>
      </div>
      {terminal || status.last_error ? (
        <Alert variant="destructive">
          <AlertDescription>{t('remoteAccess.status.lastError')}</AlertDescription>
        </Alert>
      ) : null}
      <dl className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-md border border-border bg-surface p-3">
          <dt className="text-xs text-text-muted">{t('remoteAccess.status.phase')}</dt>
          <dd role="status" className="mt-1 font-medium text-text">
            {t(`remoteAccess.status.phases.${status.phase}`)}
          </dd>
        </div>
        <div className="rounded-md border border-border bg-surface p-3">
          <dt className="text-xs text-text-muted">{t('remoteAccess.status.connector')}</dt>
          <dd role="status" className="mt-1 font-medium text-text">
            {t(`remoteAccess.status.connectors.${status.connector}`)}
          </dd>
        </div>
      </dl>
      <div className="grid gap-3 lg:grid-cols-2">
        <HostnameCard
          label={t('remoteAccess.hostnames.public')}
          hostname={status.public_hostname}
        />
        <HostnameCard label={t('remoteAccess.hostnames.warp')} hostname={status.warp_hostname} />
      </div>
      <p className="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm leading-relaxed text-text">
        {t('remoteAccess.status.direct')}
      </p>
      <p className="text-sm leading-relaxed text-text-muted">
        {t('remoteAccess.status.membership')}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={pending}
          onClick={() => {
            onAction('reconcile');
          }}
        >
          <RefreshCw aria-hidden="true" />
          {t('remoteAccess.actions.reconcile')}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={pending}
          onClick={() => {
            onAction('disable');
          }}
        >
          {pending ? <Spinner /> : null}
          {t('remoteAccess.actions.disable')}
        </Button>
        {hostname ? (
          <Button
            ref={deleteTrigger}
            type="button"
            variant="destructive"
            disabled={pending}
            onClick={() => {
              setDeleteOpen(true);
            }}
          >
            <Trash2 aria-hidden="true" />
            {t('remoteAccess.actions.delete')}
          </Button>
        ) : null}
      </div>
      <div className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4">
        <p className="text-sm leading-relaxed text-text-muted">{t('remoteAccess.rotation.body')}</p>
        <div className="flex flex-wrap gap-2">
          <a
            href="https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/"
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex min-h-[44px] items-center gap-2 rounded-md border border-border-strong px-4 py-2 text-sm font-semibold text-text hover:bg-surface-2"
          >
            <ExternalLink aria-hidden="true" className="size-4" />
            {t('remoteAccess.actions.rotate')}
          </a>
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={() => {
              onAction('refresh');
            }}
          >
            <RotateCw aria-hidden="true" />
            {t('remoteAccess.actions.refresh')}
          </Button>
        </div>
      </div>
      {terminal && onReplaceToken ? (
        <div className="flex max-w-xl flex-col gap-3 rounded-md border border-border bg-surface p-4">
          <Label htmlFor="remote-access-replacement-token">{t('remoteAccess.token.label')}</Label>
          <SecretInput
            id="remote-access-replacement-token"
            value={token}
            onChange={(event) => {
              setToken(event.target.value);
              setReplacementFailed(false);
            }}
            showLabel={t('secret.show', { label: t('remoteAccess.token.label') })}
            hideLabel={t('secret.hide', { label: t('remoteAccess.token.label') })}
            autoComplete="off"
          />
          <Button
            type="button"
            className="w-fit"
            disabled={replacing || token.trim() === ''}
            onClick={() => void replaceToken()}
          >
            {replacing ? <Spinner /> : null}
            {t('remoteAccess.token.verify')}
          </Button>
          {replacementFailed ? (
            <p role="alert" className="text-sm text-destructive">
              {t('remoteAccess.token.error')}
            </p>
          ) : null}
        </div>
      ) : null}
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={closeDelete}
        role="alertdialog"
        title={t('remoteAccess.delete.title')}
        description={t('remoteAccess.delete.body')}
        cancelLabel={t('remoteAccess.actions.cancel')}
        confirmLabel={t('remoteAccess.actions.deleteForever')}
        confirmDisabled={confirmation !== hostname}
        confirmPending={pending}
        onConfirm={async () => {
          await onDelete(confirmation);
          closeDelete(false);
        }}
      >
        <div className="grid gap-1.5">
          <Label htmlFor="remote-access-delete-confirm">
            {t('remoteAccess.delete.label', { hostname })}
          </Label>
          <Input
            id="remote-access-delete-confirm"
            value={confirmation}
            onChange={(event) => {
              setConfirmation(event.target.value);
            }}
            autoComplete="off"
          />
        </div>
      </ConfirmDialog>
    </section>
  );
}

function HostnameCard({
  label,
  hostname,
}: {
  readonly label: string;
  readonly hostname?: string | undefined;
}) {
  return (
    <div className="min-w-0 rounded-md border border-border bg-surface p-3">
      <p className="text-xs text-text-muted">{label}</p>
      {hostname ? (
        <a
          href={`https://${hostname}`}
          target="_blank"
          rel="noreferrer noopener"
          className="mt-1 block break-all text-sm font-medium text-accent-text underline underline-offset-2"
        >
          https://{hostname}
        </a>
      ) : (
        <p className="mt-1 text-sm text-text-muted">—</p>
      )}
    </div>
  );
}

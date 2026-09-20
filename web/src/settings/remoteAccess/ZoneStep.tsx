import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import type { CloudflareAccount, RemoteAccessConfiguration } from './remoteAccessApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

interface ZoneStepProps {
  readonly accounts: readonly CloudflareAccount[];
  readonly generation: number;
  readonly token?: string | undefined;
  readonly initialAccount?: string | undefined;
  readonly initialDomain?: string | undefined;
  readonly initialPublicLabel?: string | undefined;
  readonly initialWarpLabel?: string | undefined;
  readonly onSave: (configuration: RemoteAccessConfiguration) => Promise<void>;
}

export function ZoneStep({
  accounts,
  generation,
  token,
  initialAccount = '',
  initialDomain = '',
  initialPublicLabel = 'aura',
  initialWarpLabel = 'aura-warp',
  onSave,
}: ZoneStepProps) {
  const { t } = useTranslation();
  const [account, setAccount] = useState(
    initialAccount !== ''
      ? initialAccount
      : accounts.length === 1
        ? (accounts.at(0)?.id ?? '')
        : '',
  );
  const [domain, setDomain] = useState(initialDomain);
  const [publicLabel, setPublicLabel] = useState(initialPublicLabel);
  const [warpLabel, setWarpLabel] = useState(initialWarpLabel);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const cleanDomain = domain.trim().toLowerCase();
  const publicHostname = cleanDomain === '' ? '' : `${publicLabel}.${cleanDomain}`;
  const warpHostname = cleanDomain === '' ? '' : `${warpLabel}.${cleanDomain}`;

  async function save() {
    if (busy || account === '' || cleanDomain === '') return;
    setBusy(true);
    setFailed(false);
    try {
      await onSave({
        enabled: true,
        generation,
        account_id: account,
        zone_name: cleanDomain,
        public_label: publicLabel,
        warp_label: warpLabel,
        ...(token ? { api_token: token } : {}),
      });
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex max-w-xl flex-col gap-3 rounded-md border border-border bg-surface p-4">
      <Alert>
        <AlertDescription>{t('remoteAccess.domain.external')}</AlertDescription>
      </Alert>
      <p className="text-sm text-warning">{t('remoteAccess.domain.quickTunnel')}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="remote-access-account">{t('remoteAccess.domain.account')}</Label>
          <select
            id="remote-access-account"
            value={account}
            onChange={(event) => {
              setAccount(event.target.value);
            }}
            className="min-h-[44px] rounded-md border border-input bg-bg px-3 text-sm text-text"
          >
            {accounts.length > 1 ? (
              <option value="">{t('remoteAccess.domain.account')}</option>
            ) : null}
            {accounts.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name || item.id}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="remote-access-domain">{t('remoteAccess.domain.domain')}</Label>
          <Input
            id="remote-access-domain"
            value={domain}
            onChange={(event) => {
              setDomain(event.target.value);
            }}
            autoCapitalize="none"
            autoComplete="off"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="remote-access-public-label">{t('remoteAccess.domain.publicLabel')}</Label>
          <Input
            id="remote-access-public-label"
            value={publicLabel}
            onChange={(event) => {
              setPublicLabel(event.target.value);
            }}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="remote-access-warp-label">{t('remoteAccess.domain.warpLabel')}</Label>
          <Input
            id="remote-access-warp-label"
            value={warpLabel}
            onChange={(event) => {
              setWarpLabel(event.target.value);
            }}
          />
        </div>
      </div>
      <div className="rounded-md bg-surface-2 p-3 text-sm text-text-muted">
        <p className="font-medium text-text">{t('remoteAccess.domain.preview')}</p>
        <p className="break-all">https://{publicHostname || '…'}</p>
        <p className="break-all">https://{warpHostname || '…'}</p>
      </div>
      <Button
        type="button"
        className="w-fit"
        disabled={busy || account === '' || cleanDomain === ''}
        onClick={() => {
          void save();
        }}
      >
        {busy ? <Spinner /> : null}
        {t('remoteAccess.domain.save')}
      </Button>
      {failed ? (
        <p role="alert" className="text-sm text-destructive">
          {t('remoteAccess.status.lastError')}
        </p>
      ) : null}
    </div>
  );
}

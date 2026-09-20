import { useEffect, useRef, useState } from 'react';
import { KeyRound, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import type { CloudflareAccount } from './remoteAccessApi';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

interface TokenStepProps {
  readonly onVerified: (token: string) => Promise<readonly CloudflareAccount[]>;
}

export function TokenStep({ onVerified }: TokenStepProps) {
  const { t } = useTranslation();
  const [token, setToken] = useState('');
  const tokenRef = useRef('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(
    () => () => {
      tokenRef.current = '';
    },
    [],
  );

  async function verify() {
    if (token.trim() === '' || busy) return;
    setBusy(true);
    setFailed(false);
    try {
      await onVerified(token);
      setToken('');
      tokenRef.current = '';
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex max-w-xl flex-col gap-3 rounded-md border border-border bg-surface p-4">
      <div className="flex items-center gap-2 text-text">
        <KeyRound aria-hidden="true" className="size-4" />
        <Label htmlFor="remote-access-token">{t('remoteAccess.token.label')}</Label>
      </div>
      <p className="text-sm leading-relaxed text-text-muted">{t('remoteAccess.token.hint')}</p>
      <SecretInput
        id="remote-access-token"
        value={token}
        onChange={(event) => {
          setToken(event.target.value);
          tokenRef.current = event.target.value;
          setFailed(false);
        }}
        showLabel={t('secret.show', { label: t('remoteAccess.token.label') })}
        hideLabel={t('secret.hide', { label: t('remoteAccess.token.label') })}
        autoComplete="off"
      />
      <Button
        type="button"
        className="w-fit"
        disabled={busy || token.trim() === ''}
        onClick={() => void verify()}
      >
        {busy ? <Spinner /> : <ShieldCheck aria-hidden="true" />}
        {t('remoteAccess.token.verify')}
      </Button>
      {failed ? (
        <p role="alert" className="text-sm text-destructive">
          {t('remoteAccess.token.error')}
        </p>
      ) : null}
    </div>
  );
}

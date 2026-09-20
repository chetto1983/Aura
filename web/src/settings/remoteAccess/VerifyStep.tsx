import { CheckCircle2, ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import type { RemoteAccessStatusDTO } from './remoteAccessApi';
import { Button } from '@/components/ui/button';

interface VerifyStepProps {
  readonly status: RemoteAccessStatusDTO;
  readonly busy: boolean;
  readonly accepted: boolean;
  readonly onAccept: () => void;
}

export function VerifyStep({ status, busy, accepted, onAccept }: VerifyStepProps) {
  const { t } = useTranslation();
  const publicHostname = status.public_hostname;
  const onPublicHostname =
    publicHostname !== undefined &&
    window.location.hostname.toLowerCase() === publicHostname.toLowerCase();
  return (
    <div className="flex max-w-xl flex-col gap-3 rounded-md border border-border bg-surface p-4">
      <h3 className="font-semibold text-text">{t('remoteAccess.verify.heading')}</h3>
      <p className="text-sm leading-relaxed text-text-muted">{t('remoteAccess.verify.body')}</p>
      {publicHostname ? (
        <a
          href={`https://${publicHostname}/?settings=remote-access`}
          className="inline-flex min-h-[44px] w-fit items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground"
        >
          <ExternalLink aria-hidden="true" className="size-4" />
          {t('remoteAccess.verify.open')}
        </a>
      ) : null}
      {onPublicHostname ? (
        <Button type="button" className="w-fit" disabled={busy} onClick={onAccept}>
          {busy ? <Spinner /> : <CheckCircle2 aria-hidden="true" />}
          {t('remoteAccess.verify.accept')}
        </Button>
      ) : (
        <p role="status" className="text-sm text-text-muted">
          {t('remoteAccess.verify.externalOnly')}
        </p>
      )}
      {accepted ? (
        <p role="status" className="text-sm text-success">
          {t('remoteAccess.verify.accepted')}
        </p>
      ) : null}
    </div>
  );
}

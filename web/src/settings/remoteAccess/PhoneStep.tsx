import { CheckCircle2, CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { formatDateTime, parseTime } from '../../update/updateTime';
import { ManageActions, type ManageActionsProps } from './ManageActions';
import { StepFailure } from './ReasonNotice';
import { StepCard, StepFoot, StepLink } from './StepCard';
import type { StepKey } from './remoteAccessSteps';
import { useStepAction } from './useStepAction';
import { Button } from '@/components/ui/button';

interface PhoneStepProps extends ManageActionsProps {
  readonly onAccept: () => Promise<void>;
  readonly onRetry: () => Promise<void>;
  readonly onBack: () => void;
  readonly onOpenStep: (step: StepKey) => void;
}

export function PhoneStep({
  status,
  onAccept,
  onRetry,
  onDisable,
  onDelete,
  onBack,
  onOpenStep,
}: PhoneStepProps) {
  const { t, i18n } = useTranslation();
  const accept = useStepAction();
  const retry = useStepAction();
  const hostname = status.public_hostname ?? '';
  // An outage must never ask for the test again, whatever the row says about acceptance.
  const testing = !status.accepted && status.reason !== 'tunnel_down';
  // The press was refused, not the row: Conferma is the way on, and the row's sentence about an
  // outage healing with no new test is false for an address nobody has confirmed yet.
  const tooEarly = accept.failure === 'tunnel_down';
  const onPublicHostname =
    hostname !== '' && window.location.hostname.toLowerCase() === hostname.toLowerCase();
  const checkedAt = parseTime(status.last_checked_at);

  return (
    <StepCard heading={t('remoteAccess.phone.heading')}>
      {testing ? (
        <p className="text-sm leading-relaxed text-text-muted">{t('remoteAccess.phone.test')}</p>
      ) : (
        <div className="flex flex-col gap-1">
          <p role="status" className="flex items-center gap-2 font-medium text-text">
            {status.reachable ? (
              <CheckCircle2 aria-hidden="true" className="size-5 shrink-0 text-success" />
            ) : (
              <CircleAlert aria-hidden="true" className="size-5 shrink-0 text-danger" />
            )}
            {t(
              status.reachable ? 'remoteAccess.phone.reachable' : 'remoteAccess.phone.unreachable',
            )}
          </p>
          {checkedAt === undefined ? null : (
            <p className="text-[13px] text-text-muted">
              {t('remoteAccess.phone.lastChecked', {
                time: formatDateTime(checkedAt, i18n.language),
              })}
            </p>
          )}
        </div>
      )}
      <p className="font-mono text-sm break-all text-text">https://{hostname}</p>
      <StepLink
        href={`https://${hostname}/?settings=remote-access`}
        variant={testing ? 'default' : 'outline'}
      >
        {t('remoteAccess.phone.open')}
      </StepLink>
      {testing && onPublicHostname ? (
        <Button
          type="button"
          className="w-fit"
          disabled={accept.running}
          onClick={() => void accept.run(onAccept)}
        >
          {accept.running ? <Spinner /> : <CheckCircle2 aria-hidden="true" />}
          {t('remoteAccess.phone.accept')}
        </Button>
      ) : null}
      {testing && !onPublicHostname ? (
        <p className="text-sm leading-relaxed text-text-muted">
          {t('remoteAccess.phone.externalOnly')}
        </p>
      ) : null}
      {testing && tooEarly ? (
        <p role="alert" className="text-sm leading-relaxed text-danger">
          {t('remoteAccess.phone.notReady')}
        </p>
      ) : null}
      <StepFailure
        status={status}
        step="phone"
        failure={(tooEarly ? null : accept.failure) ?? retry.failure}
        onOpenStep={onOpenStep}
        onRetry={() => void retry.run(onRetry)}
        retrying={retry.running}
      />
      <p className="text-[13px] leading-relaxed text-text-muted">
        {t('remoteAccess.phone.direct')}
      </p>
      <ManageActions status={status} onDisable={onDisable} onDelete={onDelete} />
      <StepFoot onBack={onBack} />
    </StepCard>
  );
}

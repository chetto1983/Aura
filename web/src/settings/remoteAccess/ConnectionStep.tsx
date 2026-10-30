import { CheckCircle2, RotateCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { FailureLine, StepFailure } from './ReasonNotice';
import { StepCard, StepFoot, StepLink } from './StepCard';
import type { RemoteAccessStatusDTO } from './remoteAccessApi';
import { stepStates, type StepKey } from './remoteAccessSteps';
import { useStepAction } from './useStepAction';
import { Button } from '@/components/ui/button';

interface ConnectionStepProps {
  readonly status: RemoteAccessStatusDTO;
  readonly onRetry: () => Promise<void>;
  readonly onRefreshToken: () => Promise<void>;
  readonly onBack: () => void;
  readonly onNext: () => void;
  readonly onOpenStep: (step: StepKey) => void;
}

export function ConnectionStep({
  status,
  onRetry,
  onRefreshToken,
  onBack,
  onNext,
  onOpenStep,
}: ConnectionStepProps) {
  const { t } = useTranslation();
  const retry = useStepAction();
  const refresh = useStepAction();
  const state = stepStates(status).connection;
  const deleting = status.phase === 'deleting';

  function whyNotNext(): string | null {
    if (state === 'done') return null;
    return state === 'failed'
      ? t('remoteAccess.connection.needFix')
      : t('remoteAccess.connection.needWait');
  }

  return (
    <StepCard heading={t('remoteAccess.connection.heading')}>
      {deleting || state === 'active' ? (
        <p role="status" className="flex items-center gap-2 text-sm leading-relaxed text-text">
          <Spinner />
          {t(deleting ? 'remoteAccess.connection.deleting' : 'remoteAccess.connection.running')}
        </p>
      ) : null}
      {state === 'done' ? (
        <p role="status" className="flex items-center gap-2 text-sm font-medium text-text">
          <CheckCircle2 aria-hidden="true" className="size-5 shrink-0 text-success" />
          {t('remoteAccess.connection.ready')}
        </p>
      ) : null}
      <StepFailure
        status={status}
        step="connection"
        failure={retry.failure}
        onOpenStep={onOpenStep}
        onRetry={() => void retry.run(onRetry)}
        retrying={retry.running}
      />
      {state === 'done' && status.enabled ? (
        <div className="flex flex-col items-start gap-2 rounded-md bg-surface-2 p-3">
          <p className="text-sm leading-relaxed text-text-muted">
            {t('remoteAccess.connection.rotation')}
          </p>
          <div className="flex w-full flex-wrap gap-2 [&>*]:w-full [&>*]:whitespace-normal sm:[&>*]:w-auto">
            <StepLink
              href="https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/"
              external
            >
              {t('remoteAccess.connection.rotationGuide')}
            </StepLink>
            <Button
              type="button"
              variant="outline"
              disabled={refresh.running}
              onClick={() => void refresh.run(onRefreshToken)}
            >
              {refresh.running ? <Spinner /> : <RotateCw aria-hidden="true" />}
              {t('remoteAccess.connection.rotationRefresh')}
            </Button>
          </div>
          {refresh.failure === null ? null : <FailureLine failure={refresh.failure} />}
        </div>
      ) : null}
      <StepFoot onBack={onBack} why={whyNotNext()} onNext={onNext} />
    </StepCard>
  );
}

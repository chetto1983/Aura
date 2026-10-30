import { TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import type { RemoteAccessReason, RemoteAccessStatusDTO } from './remoteAccessApi';
import { reasonAction, reasonOn, type Failure, type StepKey } from './remoteAccessSteps';
import { Button } from '@/components/ui/button';

interface NoticeActions {
  readonly onOpenStep: (step: StepKey) => void;
  /** Hands the field that has to change back to the operator; only steps with fields have one. */
  readonly onFix?: () => void;
  readonly onRetry: () => void;
  readonly retrying?: boolean;
}

interface ReasonNoticeProps extends NoticeActions {
  readonly reason: Exclude<RemoteAccessReason, ''>;
  readonly step: StepKey;
}

export function ReasonNotice({
  reason,
  step,
  onOpenStep,
  onFix,
  onRetry,
  retrying = false,
}: ReasonNoticeProps) {
  const { t } = useTranslation();
  const action = reasonAction(reason, step);

  function act() {
    if (action === 'fix') onFix?.();
    else if (action === 'retry') onRetry();
    else onOpenStep(action);
  }

  return (
    <div
      role="alert"
      className="flex flex-col items-start gap-3 rounded-md border border-danger/40 bg-danger/10 p-3"
    >
      <p className="flex gap-2 text-sm leading-relaxed text-text">
        <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-danger" />
        {t(`remoteAccess.reasons.${reason}.sentence`)}
      </p>
      <Button
        type="button"
        variant="outline"
        className="whitespace-normal"
        disabled={retrying}
        onClick={act}
      >
        {retrying ? <Spinner /> : null}
        {t(`remoteAccess.reasons.${reason}.action`)}
      </Button>
    </div>
  );
}

/** One sentence for any failure, where there is no room for a notice and its button. */
export function FailureLine({ failure }: { readonly failure: Failure }) {
  const { t } = useTranslation();
  return (
    <p role="alert" className="text-sm leading-relaxed text-danger">
      {failure === 'stale' || failure === 'generic'
        ? t(`remoteAccess.failure.${failure}`)
        : t(`remoteAccess.reasons.${failure}.sentence`)}
    </p>
  );
}

interface StepFailureProps extends NoticeActions {
  readonly status: RemoteAccessStatusDTO;
  readonly step: StepKey;
  readonly failure: Failure | null;
}

/** What this step has to say. A press that failed with a reason replaces the row's; one that
 * failed with no reason sits above it, because the row's button is still the way forward. */
export function StepFailure({ status, step, failure, ...actions }: StepFailureProps) {
  const unnamed = failure === 'stale' || failure === 'generic';
  const reason = failure === null || unnamed ? reasonOn(status, step) : failure;
  return (
    <>
      {unnamed ? <FailureLine failure={failure} /> : null}
      {reason === '' ? null : <ReasonNotice reason={reason} step={step} {...actions} />}
    </>
  );
}

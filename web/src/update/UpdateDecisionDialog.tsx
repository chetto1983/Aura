import { useId, useState } from 'react';
import type { TFunction } from 'i18next';
import { CircleArrowUp, Clock3, TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { HttpError } from '../api/json';
import { Spinner } from '../components/Spinner';
import type { SystemUpdateStatus } from './systemUpdateApi';
import type { DeferOutcome } from './updateCenterContext';
import { deadlinePassed, deferredUntil, impactOf, type Impact } from './updateModel';
import {
  DEFER_CHOICES,
  deferTarget,
  formatAgo,
  formatDateTime,
  formatSpan,
  parseTime,
  shortRev,
  toRFC3339,
  wallClock,
  type DeferChoice,
} from './updateTime';
import { useApplyUpdate, useDeferUpdate } from './useSystemUpdate';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

export interface UpdateDecisionDialogProps {
  readonly status: SystemUpdateStatus;
  readonly now: number;
  readonly outcome: DeferOutcome | undefined;
  readonly onDismiss: () => void;
  readonly onDecided: (outcome?: DeferOutcome) => void;
  readonly onOutcomeClosed: () => void;
}

export function UpdateDecisionDialog({
  status,
  now,
  outcome,
  onDismiss,
  onDecided,
  onOutcomeClosed,
}: UpdateDecisionDialogProps) {
  if (outcome !== undefined) {
    return <DeferredNotice outcome={outcome} now={now} onClose={onOutcomeClosed} />;
  }
  return <DecisionDialog status={status} now={now} onDismiss={onDismiss} onDecided={onDecided} />;
}

function failureCopy(err: unknown, t: TFunction): string {
  const code = err instanceof HttpError ? err.status : 0;
  if (code === 409) return t('update.dialog.errors.conflict');
  if (code === 403) return t('update.dialog.errors.forbidden');
  if (code === 400) return t('update.dialog.errors.invalid');
  return t('update.dialog.errors.generic');
}

function deferLabel(choice: DeferChoice, t: TFunction): string {
  if (choice === 'hour') return t('update.dialog.defer.hour');
  if (choice === 'fourHours') return t('update.dialog.defer.fourHours');
  return t('update.dialog.defer.tonight');
}

function DecisionDialog({
  status,
  now,
  onDismiss,
  onDecided,
}: Pick<UpdateDecisionDialogProps, 'status' | 'now' | 'onDismiss' | 'onDecided'>) {
  const { t, i18n } = useTranslation();
  const language = i18n.language;
  const apply = useApplyUpdate();
  const defer = useDeferUpdate();
  const [failure, setFailure] = useState<string | undefined>(undefined);
  const deferLabelId = useId();
  const failed = status.state === 'failed';
  const mandatory = deadlinePassed(status, now);
  const deadline = parseTime(status.deadline);
  const builtAt = parseTime(status.available_built_at);
  const pendingSince = parseTime(status.pending_since);
  const heldUntil = deferredUntil(status, now);
  const busy = apply.isPending || defer.isPending;

  async function runApply() {
    setFailure(undefined);
    try {
      await apply.mutateAsync();
      onDecided();
    } catch (err) {
      setFailure(failureCopy(err, t));
    }
  }

  async function runDefer(choice: DeferChoice) {
    setFailure(undefined);
    const asked = toRFC3339(deferTarget(choice, new Date()));
    try {
      const res = await defer.mutateAsync(asked);
      const until = parseTime(res.deferred_until) ?? Date.parse(asked);
      onDecided({ until, clamped: until < Date.parse(asked) });
    } catch (err) {
      setFailure(failureCopy(err, t));
    }
  }

  // The daemon clamps a deferral to the deadline, so the preview does too.
  function previewTime(choice: DeferChoice): string {
    const target = deferTarget(choice, new Date(now)).getTime();
    const shown = deadline === undefined ? target : Math.min(target, deadline);
    return wallClock(shown, now, language).time;
  }

  const headline = mandatory
    ? t('update.dialog.mandatory')
    : deadline === undefined
      ? t('update.dialog.firstPause')
      : t('update.dialog.deadline', { ...wallClock(deadline, now, language) });

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onDismiss();
      }}
    >
      <DialogContent className="update-dialog w-[calc(100vw-2rem)] max-w-[34rem] gap-0 overflow-hidden p-0">
        <DialogHeader className="update-dialog__hero gap-3 px-6 pb-5 pt-6 pr-16">
          <span className="update-dialog__mark" aria-hidden="true">
            <CircleArrowUp />
          </span>
          <DialogTitle className="text-[24px] leading-tight">
            {t('update.dialog.title')}
          </DialogTitle>
          <DialogDescription className="text-[14px] leading-relaxed text-text-muted">
            {headline}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 px-6 py-5">
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)] items-baseline gap-x-5 gap-y-2 text-[13px]">
            <dt className="text-text-faint">{t('update.dialog.version')}</dt>
            <dd>
              <code className="update-rev">{shortRev(status.available_rev)}</code>
            </dd>
            {builtAt === undefined ? null : (
              <>
                <dt className="text-text-faint">{t('update.dialog.built')}</dt>
                <dd className="text-text">{formatDateTime(builtAt, language)}</dd>
              </>
            )}
            {pendingSince === undefined ? null : (
              <>
                <dt className="text-text-faint">{t('update.dialog.waiting')}</dt>
                <dd className="text-text">{formatSpan(now - pendingSince, language)}</dd>
              </>
            )}
          </dl>

          {heldUntil === undefined ? null : (
            <p className="flex items-center gap-2 text-[13px] text-text-muted">
              <Clock3 aria-hidden="true" className="size-4 shrink-0 text-info" />
              {t('update.dialog.deferred', { ...wallClock(heldUntil, now, language) })}
            </p>
          )}

          <ImpactLine impact={impactOf(status, now)} language={language} />

          {failed ? (
            <Alert variant="destructive">
              <TriangleAlert aria-hidden="true" />
              <AlertDescription>
                <p className="font-semibold text-danger">{t('update.dialog.failed')}</p>
                {status.error === '' ? null : (
                  <p className="break-words font-mono text-[12px]">{status.error}</p>
                )}
              </AlertDescription>
            </Alert>
          ) : null}

          {failure === undefined ? null : (
            <p role="alert" className="text-[13px] text-danger">
              {failure}
            </p>
          )}
        </div>

        <DialogFooter className="update-dialog__footer justify-between gap-3 border-t border-border px-6 py-4">
          {mandatory ? null : (
            <div
              role="group"
              aria-labelledby={deferLabelId}
              className="flex flex-wrap items-center gap-2"
            >
              <span
                id={deferLabelId}
                className="text-[11px] font-semibold uppercase tracking-[0.14em] text-text-faint"
              >
                {t('update.dialog.defer.label')}
              </span>
              {DEFER_CHOICES.map((choice) => (
                <Button
                  key={choice}
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() => void runDefer(choice)}
                  className="gap-1.5 rounded-full"
                >
                  {deferLabel(choice, t)}
                  <span className="font-mono text-[11px] font-normal text-text-faint">
                    {previewTime(choice)}
                  </span>
                </Button>
              ))}
            </div>
          )}
          <div className="ml-auto flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onDismiss}>
              {mandatory ? t('update.dialog.close') : t('update.dialog.later')}
            </Button>
            <Button
              type="button"
              disabled={busy}
              aria-busy={apply.isPending}
              onClick={() => void runApply()}
            >
              {apply.isPending ? <Spinner /> : <CircleArrowUp aria-hidden="true" />}
              {failed ? t('update.dialog.retry') : t('update.dialog.apply')}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ImpactLine({ impact, language }: { readonly impact: Impact; readonly language: string }) {
  const { t } = useTranslation();
  const text =
    impact.kind === 'running'
      ? t('update.dialog.impact.running', { count: impact.count })
      : impact.kind === 'recent'
        ? t('update.dialog.impact.recent', { ago: formatAgo(impact.agoMs, language) })
        : t('update.dialog.impact.idle');
  return (
    <div
      data-kind={impact.kind}
      className="update-impact flex items-center gap-3 rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 py-2.5"
    >
      <span className="update-impact__dot" aria-hidden="true" />
      <p className="flex min-w-0 flex-col leading-snug">
        <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-text-faint">
          {t('update.dialog.impact.label')}
        </span>
        <span className="text-[14px] text-text">{text}</span>
      </p>
    </div>
  );
}

function DeferredNotice({
  outcome,
  now,
  onClose,
}: {
  readonly outcome: DeferOutcome;
  readonly now: number;
  readonly onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="update-dialog w-[calc(100vw-2rem)] max-w-sm gap-4 p-6">
        <DialogHeader className="gap-2 pr-10">
          <DialogTitle>{t('update.deferred.title')}</DialogTitle>
          <DialogDescription className="text-[14px] text-text-muted">
            {t('update.deferred.body', { ...wallClock(outcome.until, now, i18n.language) })}
          </DialogDescription>
        </DialogHeader>
        {outcome.clamped ? (
          <p className="text-[13px] text-warning">{t('update.deferred.clamped')}</p>
        ) : null}
        <DialogFooter>
          <Button type="button" onClick={onClose}>
            {t('update.deferred.ok')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

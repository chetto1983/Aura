import { useId, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { HttpError } from '../api/json';
import { Spinner } from '../components/Spinner';
import {
  installPlugin,
  reviewPlugin,
  type PluginInstallRequest,
  type PluginReport,
  type PluginStep,
} from './pluginApi';
import { PluginReviewCard } from './PluginReviewCard';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// PluginInstallPanel — review a plugin by its owner/repo/plugin name, then install exactly what
// the review showed, with one confirmation. The install sends the review's own ref, commit and
// digest, never the field's text: the server refuses (409) when what it would write no longer
// matches the review, and the operator reviews again. Editing the name drops the review, so a
// review can never be installed under a name it was not made for.

export interface PluginInstallPanelProps {
  /** The installed plugin names: a review of one of them cannot be installed again. */
  readonly installedNames: readonly string[];
  /** Opens a connector on the MCP board, where its authorization lives. */
  readonly onOpenServer: (name: string) => void;
}

export function PluginInstallPanel({ installedNames, onOpenServer }: PluginInstallPanelProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();
  const refId = useId();
  const [ref, setRef] = useState('');

  const review = useMutation({ mutationFn: (target: string) => reviewPlugin(target) });
  // Whatever the install's end, the boards it touches refresh: a failure can still have
  // landed some skills, and an install leaves a record either way.
  const install = useMutation({
    mutationFn: (req: PluginInstallRequest) => installPlugin(req),
    onSettled: () => {
      for (const board of ['plugins', 'skills', 'mcp']) {
        void queryClient.invalidateQueries({ queryKey: ['governance', board] });
      }
    },
  });

  const reviewed = review.data;
  const alreadyInstalled = reviewed !== undefined && installedNames.includes(reviewed.name);
  const digestChanged =
    install.error instanceof HttpError && install.error.status === 409 && !alreadyInstalled;

  function startReview(target: string) {
    install.reset();
    review.mutate(target);
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h3 id={headingId} className="font-display text-[20px] font-semibold text-text">
        {t('governance.plugins.install.heading')}
      </h3>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          const trimmed = ref.trim();
          if (trimmed !== '' && !review.isPending) startReview(trimmed);
        }}
        className="flex flex-col gap-1"
      >
        <Label htmlFor={refId} className="text-[13px] font-semibold text-text">
          {t('governance.plugins.install.refLabel')}
        </Label>
        <div className="flex flex-wrap gap-2">
          <Input
            id={refId}
            type="text"
            value={ref}
            onChange={(event) => {
              setRef(event.target.value);
              review.reset();
              install.reset();
            }}
            placeholder="owner/repo/plugin"
            aria-describedby={`${refId}-hint`}
            autoComplete="off"
            spellCheck={false}
            className="min-w-0 flex-1 font-mono text-[13px]"
          />
          <Button
            type="submit"
            variant="outline"
            disabled={ref.trim() === '' || review.isPending}
            aria-busy={review.isPending}
          >
            {review.isPending ? <Spinner /> : null}
            {t('governance.plugins.install.review')}
          </Button>
        </div>
        <p id={`${refId}-hint`} className="text-[13px] text-text-muted">
          {review.isPending
            ? t('governance.plugins.install.reviewing')
            : t('governance.plugins.install.refHint')}
        </p>
      </form>

      {review.isError ? (
        <Alert variant="destructive">
          <AlertDescription>
            {failureReason(review.error, t('governance.plugins.install.reviewFailed'))}
          </AlertDescription>
        </Alert>
      ) : null}

      {reviewed !== undefined ? (
        <>
          <PluginReviewCard review={reviewed} />

          {alreadyInstalled ? (
            <Alert>
              <AlertDescription>
                {t('governance.plugins.install.alreadyInstalled', { name: reviewed.name })}
              </AlertDescription>
            </Alert>
          ) : null}

          {digestChanged ? (
            <Alert variant="destructive">
              <AlertDescription className="gap-3">
                <p>{t('governance.plugins.install.digestChanged')}</p>
                <Button
                  type="button"
                  variant="outline"
                  className="self-start"
                  onClick={() => {
                    startReview(reviewed.ref);
                  }}
                >
                  {t('governance.plugins.install.reviewAgain')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : install.isError && !alreadyInstalled ? (
            <Alert variant="destructive">
              <AlertDescription>
                {failureReason(install.error, t('governance.plugins.install.installFailed'))}
              </AlertDescription>
            </Alert>
          ) : null}

          {install.data === undefined ? (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                disabled={alreadyInstalled || install.isPending || digestChanged}
                aria-busy={install.isPending}
                onClick={() => {
                  install.mutate({
                    ref: reviewed.ref,
                    commit: reviewed.commit,
                    digest: reviewed.digest,
                  });
                }}
              >
                {install.isPending ? <Spinner /> : null}
                {t('governance.plugins.install.submit')}
              </Button>
              {install.isPending ? (
                <p className="text-[13px] text-text-muted">
                  {t('governance.plugins.install.installing')}
                </p>
              ) : null}
            </div>
          ) : (
            <InstallReport report={install.data} onOpenServer={onOpenServer} />
          )}
        </>
      ) : null}
    </section>
  );
}

// failureReason prefers the server's own sentence, which says what to change; the panel's
// fallback covers a failure that carried none.
function failureReason(error: unknown, fallback: string): string {
  return error instanceof HttpError && error.reason !== '' ? error.reason : fallback;
}

const OUTCOME_VARIANT: Readonly<Record<string, BadgeProps['variant']>> = {
  ok: 'success',
  skipped: 'secondary',
  failed: 'danger',
  attention: 'warning',
};

function InstallReport({
  report,
  onOpenServer,
}: {
  readonly report: PluginReport;
  readonly onOpenServer: (name: string) => void;
}) {
  const { t } = useTranslation();
  const heading = t('governance.plugins.report.heading');
  const steps: readonly { step: PluginStep; connector: boolean }[] = [
    ...report.skills.map((step) => ({ step, connector: false })),
    ...report.connectors.map((step) => ({ step, connector: true })),
  ];

  return (
    <section className="flex flex-col gap-2">
      {report.error !== '' ? (
        <Alert variant="destructive">
          <AlertDescription>
            {t('governance.plugins.report.stopped', { reason: report.error })}
          </AlertDescription>
        </Alert>
      ) : null}
      <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
        {heading}
      </h4>
      <ul aria-label={heading} className="flex flex-col gap-1.5">
        {steps.map(({ step, connector }) => (
          <li
            key={`${connector ? 'connector' : 'skill'}:${step.name}`}
            className="flex flex-col gap-1 rounded-md border border-border bg-surface px-3 py-2 text-[13px]"
          >
            <span className="flex flex-wrap items-center gap-2">
              <span className="break-all font-mono text-text">{step.name}</span>
              <Badge variant={OUTCOME_VARIANT[step.outcome] ?? 'secondary'}>
                {t(`governance.plugins.report.outcome.${step.outcome}`, {
                  defaultValue: step.outcome,
                })}
              </Badge>
            </span>
            {step.reason !== '' ? (
              <span className="break-words text-text-muted">{step.reason}</span>
            ) : null}
            {connector && step.outcome === 'attention' ? (
              <Button
                type="button"
                variant="outline"
                className="self-start"
                onClick={() => {
                  onOpenServer(step.name);
                }}
              >
                {t('governance.plugins.report.openServer')}
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useCapabilities } from '../admin/useAdmin';
import { ModelSettingsPanel } from '../settings/ModelSettingsPanel';
import type { SaveOutcome } from '../settings/modelSettingsState';
import { browserRestartDeps, requestRestart, watchRestart } from '../settings/restartAura';
import { fetchOnboardingStatus } from './onboardingApi';
import { summarizeOpenRouterKeys, type OpenRouterKeysSummary } from './routeStepModel';
import { Button } from '@/components/ui/button';

// RouteStep is the first-run step where an admin decides what the deployment's model runs on:
// OpenRouter, with the management key Aura mints every other key from, or a local server. It is
// the Settings routing pane plus what that pane cannot say: which keys the save minted, what
// OpenRouter refused, and, the first time the services key exists, one restart so speech,
// embeddings and vision pick it up. The setup resumes here once the daemon is back.

type Phase = 'form' | 'restarting' | 'ready' | 'restartFailed';

export interface RouteStepProps {
  /** The daemon's routeRequired: while true there is no Skip, and the setup cannot be closed. */
  readonly required: boolean;
  readonly onRequiredChange: (required: boolean) => void;
  readonly onDone: () => void;
}

export function RouteStep({ required, onRequiredChange, onDone }: RouteStepProps) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const [phase, setPhase] = useState<Phase>('form');
  const [summary, setSummary] = useState<OpenRouterKeysSummary | undefined>(undefined);
  const [stillRequired, setStillRequired] = useState(false);

  // The page stays up through the restart: the watch's "back" callback moves the step on instead
  // of reloading, so the setup keeps what the operator already filled in.
  useEffect(() => {
    if (phase !== 'restarting') return undefined;
    return watchRestart(
      {
        ...browserRestartDeps,
        reload: () => {
          setPhase('ready');
        },
      },
      () => {
        setPhase('restartFailed');
      },
    );
  }, [phase]);

  async function restart() {
    try {
      setPhase((await requestRestart()) === 'accepted' ? 'restarting' : 'restartFailed');
    } catch {
      setPhase('restartFailed');
    }
  }

  async function handleSaved(outcome?: SaveOutcome) {
    const keys = summarizeOpenRouterKeys(outcome?.openRouterKeys ?? [], identityId);
    let missing = false;
    if (required) {
      // A status read that fails lets the admin go on: a turn still refuses and says what is
      // missing, which beats a setup nobody can leave.
      missing = await fetchOnboardingStatus().then(
        (status) => status.routeRequired,
        () => false,
      );
      onRequiredChange(missing);
    }
    setSummary(keys);
    setStillRequired(missing);
    if (keys.errors.length > 0 || missing) return;
    if (keys.servicesLabel !== '') {
      await restart();
      return;
    }
    if (keys.ownLabel !== '') {
      setPhase('ready');
      return;
    }
    onDone();
  }

  const summaryBlock =
    summary === undefined ? null : <KeysSummary summary={summary} stillRequired={stillRequired} />;

  if (phase === 'restarting') {
    return (
      <div className="flex flex-col gap-4">
        {summaryBlock}
        <div
          role="status"
          className="flex items-start gap-3 rounded-md border border-border bg-surface px-4 py-3"
        >
          <Loader2
            aria-hidden="true"
            className="mt-0.5 size-4 shrink-0 animate-spin text-text-muted"
          />
          <div className="flex flex-col gap-1">
            <p className="text-sm font-semibold text-text">
              {t('onboarding.profile.route.restarting')}
            </p>
            <p className="text-[13px] text-text-muted">
              {t('onboarding.profile.route.restartingBody')}
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (phase !== 'form') {
    return (
      <div className="flex flex-col gap-4">
        {summaryBlock}
        {phase === 'restartFailed' ? (
          <p role="alert" className="text-[13px] text-warning">
            {t('onboarding.profile.route.restartFailed')}
          </p>
        ) : null}
        <div>
          <Button type="button" onClick={onDone} className="px-6">
            {t('onboarding.profile.route.continue')}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <ModelSettingsPanel
        groups={['routing']}
        saveLabel={t('onboarding.profile.route.save')}
        skipLabel={t('onboarding.profile.route.skip')}
        skippable={!required}
        onComplete={handleSaved}
      />
      {summaryBlock}
    </div>
  );
}

function KeysSummary({
  summary,
  stillRequired,
}: {
  readonly summary: OpenRouterKeysSummary;
  readonly stillRequired: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 text-[13px]">
      {summary.ownLabel === '' ? null : (
        <p className="text-text">
          {t('onboarding.profile.route.ownKey', { label: summary.ownLabel })}
        </p>
      )}
      {summary.servicesLabel === '' ? null : (
        <p className="text-text">
          {t('onboarding.profile.route.servicesKey', { label: summary.servicesLabel })}
        </p>
      )}
      {summary.errors.length > 0 ? (
        <div
          role="alert"
          className="rounded-md border border-danger bg-danger/10 px-4 py-3 text-danger"
        >
          <p>{t('onboarding.profile.route.errors')}</p>
          <ul className="mt-1 list-disc pl-5 font-mono text-[12px]">
            {summary.errors.map((error) => (
              <li key={error}>{error}</li>
            ))}
          </ul>
        </div>
      ) : stillRequired ? (
        <p role="alert" className="text-danger">
          {t('onboarding.profile.route.stillRequired')}
        </p>
      ) : null}
    </div>
  );
}

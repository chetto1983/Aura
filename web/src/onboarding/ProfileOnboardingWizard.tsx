import { useCallback, useEffect, useId, useState, type ReactNode } from 'react';
import { CheckCircle2, Circle, Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { ModelSettingsPanel } from '../settings/ModelSettingsPanel';
import { InterviewStep } from './InterviewStep';
import { OnboardingCenteredState, OnboardingDialog } from './OnboardingDialog';
import { TelegramLinkStep } from './TelegramLinkStep';
import { isAuthError } from './onboardingWizardModel';
import {
  completeProfileOnboarding,
  startProfileOnboarding,
  type OnboardingProfileComplete,
  type OnboardingStart,
  type OnboardingStepResponse,
} from './onboardingApi';
import { useOnboardingStepDispatch } from './onboardingStepDispatch';
import { Button } from '@/components/ui/button';

export interface ProfileOnboardingWizardProps {
  readonly onClose: () => void;
}

type StartStatus = 'starting' | 'ready' | 'error' | 'error-auth';
type CompletionStatus = 'idle' | 'saving' | 'completed' | 'skipped' | 'error';
type ProfileStepKey = 'identity' | 'work' | 'projects' | 'social' | 'style' | 'draft' | 'runtime';

const profileStepOrder = [
  'identity',
  'work',
  'projects',
  'social',
  'style',
  'draft',
  'runtime',
] as const;
const profileStepSet = new Set<string>(profileStepOrder);

function profileStepKey(step?: OnboardingStepResponse): ProfileStepKey {
  if (step === undefined) return 'identity';
  if (step.status === 'draft' || (step.draft ?? '').trim() !== '') return 'draft';
  return profileStepSet.has(step.step) ? (step.step as ProfileStepKey) : 'identity';
}

function profileStepIndex(step: ProfileStepKey): number {
  const index = profileStepOrder.indexOf(step);
  return index < 0 ? 0 : index;
}

function localizedProfileStep(step: OnboardingStepResponse, t: TFunction): OnboardingStepResponse {
  const key = profileStepKey(step);
  return {
    ...step,
    content: t(`onboarding.profile.prompts.${key}`),
  };
}

function ProfileProgress({
  activeStep,
  t,
}: {
  readonly activeStep: ProfileStepKey;
  readonly t: TFunction;
}) {
  const activeIndex = profileStepIndex(activeStep);
  return (
    <aside className="hidden w-72 shrink-0 border-r border-border bg-surface px-5 py-6 lg:flex lg:flex-col">
      <div className="flex flex-col gap-2">
        <p className="text-xs font-semibold uppercase text-accent-text">
          {t('onboarding.profile.kicker')}
        </p>
        <h2 className="font-display text-[22px] font-semibold text-text">
          {t('onboarding.profile.heading')}
        </h2>
        <p className="text-sm leading-relaxed text-text-muted">{t('onboarding.profile.body')}</p>
      </div>
      <ol aria-label={t('onboarding.profile.progressLabel')} className="mt-7 flex flex-col gap-3">
        {profileStepOrder.map((step, index) => {
          const done = index < activeIndex;
          const active = index === activeIndex;
          return (
            <li key={step} className="flex items-start gap-3">
              <span
                className={`mt-0.5 grid size-6 shrink-0 place-items-center rounded-full border ${
                  done
                    ? 'border-accent bg-accent text-accent-foreground'
                    : active
                      ? 'border-accent text-accent-text'
                      : 'border-border text-text-muted'
                }`}
              >
                {done ? (
                  <CheckCircle2 aria-hidden="true" className="size-4" />
                ) : active ? (
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                ) : (
                  <Circle aria-hidden="true" className="size-3" />
                )}
              </span>
              <span className="min-w-0">
                <span className="block text-sm font-semibold text-text">
                  {t(`onboarding.profile.steps.${step}.label`)}
                </span>
                <span className="block text-[13px] leading-relaxed text-text-muted">
                  {t(`onboarding.profile.steps.${step}.help`)}
                </span>
              </span>
            </li>
          );
        })}
      </ol>
    </aside>
  );
}

export default function ProfileOnboardingWizard({ onClose }: ProfileOnboardingWizardProps) {
  const { t } = useTranslation();
  const titleId = useId();
  const introId = useId();

  const [startStatus, setStartStatus] = useState<StartStatus>('starting');
  const [sessionToken, setSessionToken] = useState('');
  const [interview, setInterview] = useState<OnboardingStepResponse | undefined>(undefined);
  const [stepBusy, setStepBusy] = useState(false);
  const [completionStatus, setCompletionStatus] = useState<CompletionStatus>('idle');
  const [completionResult, setCompletionResult] = useState<OnboardingProfileComplete | undefined>(
    undefined,
  );
  const [beginNonce, setBeginNonce] = useState(0);
  const [runtimeSetupOpen, setRuntimeSetupOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function run() {
      try {
        const start: OnboardingStart = await startProfileOnboarding();
        if (cancelled) return;
        setSessionToken(start.sessionToken);
        setInterview({ content: start.content, step: start.step, status: start.status });
        setCompletionStatus('idle');
        setCompletionResult(undefined);
        setRuntimeSetupOpen(false);
        setStartStatus('ready');
      } catch (err) {
        if (cancelled) return;
        setStartStatus(isAuthError(err) ? 'error-auth' : 'error');
      }
    }
    void run();
    return () => {
      cancelled = true;
    };
  }, [beginNonce]);

  const retryStart = useCallback(() => {
    setStartStatus('starting');
    setBeginNonce((n) => n + 1);
  }, []);

  const finishProfile = useCallback(async () => {
    if (sessionToken === '' || completionStatus === 'saving') return;
    setCompletionStatus('saving');
    try {
      const done = await completeProfileOnboarding(sessionToken);
      setCompletionResult(done);
      setCompletionStatus(done.skipped ? 'skipped' : 'completed');
    } catch (err) {
      if (isAuthError(err)) {
        setStartStatus('error-auth');
        return;
      }
      setCompletionStatus('error');
    }
  }, [completionStatus, sessionToken]);

  const handleStepTerminal = useCallback(() => {
    setRuntimeSetupOpen(true);
  }, []);

  const handleStepError = useCallback((err: unknown) => {
    setStartStatus(isAuthError(err) ? 'error-auth' : 'error');
  }, []);

  const dispatchStep = useOnboardingStepDispatch({
    sessionToken,
    setBusy: setStepBusy,
    onResponse: setInterview,
    onTerminal: handleStepTerminal,
    onError: handleStepError,
  });

  const activeStep: ProfileStepKey = runtimeSetupOpen ? 'runtime' : profileStepKey(interview);
  const activeIndex = profileStepIndex(activeStep);
  const displayedInterview =
    interview === undefined ? undefined : localizedProfileStep(interview, t);

  const overlay = (children: ReactNode) => (
    <OnboardingDialog
      titleId={titleId}
      describedBy={introId}
      kicker={t('onboarding.profile.kicker')}
      title={t('onboarding.profile.title')}
      closeLabel={t('onboarding.close')}
      onClose={onClose}
    >
      {children}
    </OnboardingDialog>
  );

  if (startStatus === 'starting') {
    return overlay(<OnboardingCenteredState message={t('onboarding.starting')} muted />);
  }

  if (startStatus === 'error-auth') {
    return overlay(<OnboardingCenteredState role="alert" message={t('onboarding.authExpired')} />);
  }

  if (startStatus === 'error') {
    return overlay(
      <OnboardingCenteredState
        role="alert"
        message={t('onboarding.backendUnavailable')}
        retryLabel={t('onboarding.retry')}
        onRetry={retryStart}
      />,
    );
  }

  if (completionStatus === 'saving') {
    return overlay(<OnboardingCenteredState message={t('onboarding.profile.saving')} muted />);
  }

  if (completionStatus === 'completed' || completionStatus === 'skipped') {
    return overlay(
      <div className="min-h-0 flex-1 overflow-y-auto p-8 text-center">
        <div className="mx-auto flex max-w-md flex-col items-center gap-5">
          <h2 className="font-display text-[22px] font-semibold text-text">
            {t('onboarding.profile.completeHeading')}
          </h2>
          <p className="text-[15.5px] leading-relaxed text-text-muted">
            {completionStatus === 'skipped'
              ? t('onboarding.profile.skippedBody')
              : t('onboarding.profile.completeBody')}
          </p>
          <TelegramLinkStep
            sessionToken={sessionToken}
            deepLink={completionResult?.deepLink}
            qrSvg={completionResult?.qrSvg}
            polling={completionResult?.deepLink !== undefined}
          />
          <Button type="button" variant="outline" onClick={onClose} className="px-6">
            {t('onboarding.complete.done')}
          </Button>
        </div>
      </div>,
    );
  }

  return overlay(
    <div className="flex min-h-0 flex-1">
      <ProfileProgress activeStep={activeStep} t={t} />
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="border-b border-border bg-surface px-4 py-4 lg:hidden">
          <p className="text-sm font-semibold text-text">
            {t('onboarding.profile.currentStep', {
              current: activeIndex + 1,
              total: profileStepOrder.length,
            })}
          </p>
          <div className="mt-3 h-2 overflow-hidden rounded-full bg-surface-2">
            <div
              className="h-full rounded-full bg-accent"
              style={{
                width: `${String(Math.round(((activeIndex + 1) / profileStepOrder.length) * 100))}%`,
              }}
            />
          </div>
        </div>
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-7 px-4 py-6 sm:px-6 lg:px-10 lg:py-10">
          <section className="flex flex-col gap-2">
            <p className="hidden text-sm font-semibold text-accent-text lg:block">
              {t('onboarding.profile.currentStep', {
                current: activeIndex + 1,
                total: profileStepOrder.length,
              })}
            </p>
            <h2 className="font-display text-[24px] font-semibold text-text">
              {t(`onboarding.profile.steps.${activeStep}.label`)}
            </h2>
            <p id={introId} className="max-w-2xl text-[15.5px] leading-relaxed text-text-muted">
              {t(`onboarding.profile.steps.${activeStep}.help`)}
            </p>
          </section>

          {completionStatus === 'error' ? (
            <div role="alert" className="flex flex-wrap items-center justify-between gap-3">
              <p className="text-[15.5px] text-danger">{t('onboarding.profile.saveError')}</p>
              <Button type="button" variant="outline" onClick={() => void finishProfile()}>
                {t('onboarding.retry')}
              </Button>
            </div>
          ) : null}

          {displayedInterview !== undefined ? (
            runtimeSetupOpen ? (
              <ModelSettingsPanel
                saveLabel={t('onboarding.profile.runtime.save')}
                skipLabel={t('onboarding.profile.runtime.skip')}
                onComplete={() => finishProfile()}
              />
            ) : (
              <InterviewStep
                step={displayedInterview}
                busy={stepBusy}
                onAnswer={(text) => {
                  void dispatchStep('answer', text);
                }}
                onConfirm={() => {
                  void dispatchStep('confirm');
                }}
                onEdit={(answers) => {
                  void dispatchStep('edit', undefined, answers);
                }}
                onSkip={() => {
                  void dispatchStep('skip');
                }}
                skipLabel={t('onboarding.profile.skipSetup')}
              />
            )
          ) : null}
        </div>
      </main>
    </div>,
  );
}

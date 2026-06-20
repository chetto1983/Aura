import { useCallback, useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { CapabilityPicker } from './CapabilityPicker';
import { CredentialStep } from './CredentialStep';
import { InterviewStep } from './InterviewStep';
import { OnboardingStepper } from './OnboardingStepper';
import { OnboardingWizardNav } from './OnboardingWizardNav';
import { ReviewStep } from './ReviewStep';
import { TelegramLinkStep } from './TelegramLinkStep';
import {
  credentialsValid,
  isAuthError,
  isForbiddenError,
  isTerminalStatus,
  phaseIndex as phaseIndexOf,
  provisionErrorKind,
  type Phase,
  type ProvisionErrorKind,
} from './onboardingWizardModel';
import {
  provisionOnboarding,
  startOnboarding,
  stepOnboarding,
  type OnboardingAnswers,
  type OnboardingProvisionResponse,
  type OnboardingStart,
  type OnboardingStepResponse,
} from './onboardingApi';

// OnboardingWizard is the lazy default export the AppShell mounts as a FULL-SCREEN overlay (D-04 —
// NOT a governance tab, NOT a MODES entry). It runs the linear flow over the Plan-05 endpoints:
//   credentials → capabilities → interview (5-step LoopAgent /step) → review → create (/provision)
//   → Telegram link (deep-link + QR + /telegram-status poll) + completion.
// It holds the sessionToken from /start and the accumulated inputs (email/password/capabilities/
// link-Telegram), and reuses the GraphExplorer ViewStatus + error-auth contract: a /start that
// REJECTS with HTTP 401 renders a VISIBLE auth-error (never a blank), any other failure the error
// state with retry (T-28-06-04). Secrets never render: the password is write-only (CredentialStep)
// and the bot token never enters the DOM (TelegramLinkStep renders only the deep-link + QR).
//
// Linear stepper on desktop; a compact "Step N of M" indicator on mobile. Full-screen single-
// column below lg; 44px targets; visible focus rings; React-escaped text only.

export interface OnboardingWizardProps {
  /** Dismiss the full-screen overlay (the shell trigger toggles it). */
  readonly onClose: () => void;
}

type StartStatus = 'starting' | 'ready' | 'error' | 'error-auth' | 'error-forbidden';

export default function OnboardingWizard({ onClose }: OnboardingWizardProps) {
  const { t } = useTranslation();
  const titleId = useId();

  const [startStatus, setStartStatus] = useState<StartStatus>('starting');
  const [phase, setPhase] = useState<Phase>('credentials');

  const [sessionToken, setSessionToken] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [capabilityOptions, setCapabilityOptions] = useState<readonly string[]>([]);
  const [selectedCaps, setSelectedCaps] = useState<ReadonlySet<string>>(new Set());
  const [linkTelegram, setLinkTelegram] = useState(true);

  const [interview, setInterview] = useState<OnboardingStepResponse | undefined>(undefined);
  const [stepBusy, setStepBusy] = useState(false);

  const [provisioning, setProvisioning] = useState(false);
  const [provisionError, setProvisionError] = useState<ProvisionErrorKind | undefined>(undefined);
  const [provisionResult, setProvisionResult] = useState<OnboardingProvisionResponse | undefined>(
    undefined,
  );

  // beginNonce drives (re)running /start: the mount effect runs it once, and the Retry button bumps
  // the nonce (after re-setting 'starting') to re-run. Channelling /start through an
  // effect+cancellation guard — rather than a bare setState-only effect — keeps the render pure and
  // satisfies react-hooks/set-state-in-effect (the effect synchronises with an external API and
  // cleans up its in-flight result, the sanctioned fetch-in-effect shape).
  const [beginNonce, setBeginNonce] = useState(0);

  useEffect(() => {
    let cancelled = false;
    async function run() {
      try {
        const start: OnboardingStart = await startOnboarding();
        if (cancelled) return;
        setSessionToken(start.sessionToken);
        setCapabilityOptions(start.capabilityOptions);
        setInterview({ content: start.content, step: start.step, status: start.status });
        setStartStatus('ready');
      } catch (err) {
        if (cancelled) return;
        setStartStatus(
          isAuthError(err) ? 'error-auth' : isForbiddenError(err) ? 'error-forbidden' : 'error',
        );
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

  const toggleCap = useCallback((name: string) => {
    setSelectedCaps((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  }, []);

  // ---- interview dispatch (one /step call per intent; the wizard owns busy/auth state) ----
  const dispatchStep = useCallback(
    async (
      intent: 'answer' | 'confirm' | 'edit' | 'skip',
      text?: string,
      answers?: OnboardingAnswers,
    ) => {
      if (sessionToken === '') return;
      setStepBusy(true);
      try {
        const resp = await stepOnboarding(sessionToken, {
          intent,
          ...(text !== undefined ? { text } : {}),
          ...(answers !== undefined ? { answers } : {}),
        });
        setInterview(resp);
        if (isTerminalStatus(resp.status)) {
          setPhase('review');
        }
      } catch (err) {
        setStartStatus(
          isAuthError(err) ? 'error-auth' : isForbiddenError(err) ? 'error-forbidden' : 'error',
        );
      } finally {
        setStepBusy(false);
      }
    },
    [sessionToken],
  );

  const create = useCallback(async () => {
    if (sessionToken === '') return;
    setProvisioning(true);
    setProvisionError(undefined);
    try {
      const result = await provisionOnboarding(sessionToken, {
        email,
        password,
        capabilities: [...selectedCaps],
        linkTelegram,
      });
      setProvisionResult(result);
      // Password lives only until the saga ran; clear it so it cannot linger in state.
      setPassword('');
      setPhase('complete');
    } catch (err) {
      if (isAuthError(err)) {
        setStartStatus('error-auth');
        return;
      }
      setProvisionError(provisionErrorKind(err));
    } finally {
      setProvisioning(false);
    }
  }, [sessionToken, email, password, selectedCaps, linkTelegram]);

  const canAdvanceCredentials = credentialsValid(email, password);
  const phaseIndex = phaseIndexOf(phase);

  const overlay = (children: React.ReactNode) => (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      className="fixed inset-0 z-50 flex flex-col bg-bg text-text"
    >
      <header className="flex shrink-0 items-center justify-between gap-2 border-b border-border bg-surface px-4 py-3">
        <h1 id={titleId} className="font-display text-[18px] font-semibold text-text">
          {t('onboarding.title')}
        </h1>
        <button
          type="button"
          aria-label={t('onboarding.close')}
          onClick={onClose}
          className="flex min-h-[44px] min-w-[44px] items-center justify-center rounded-md text-text-muted outline-none transition-colors hover:bg-surface-2 hover:text-text focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span aria-hidden="true" className="text-lg">
            ✕
          </span>
        </button>
      </header>
      {children}
    </div>
  );

  if (startStatus === 'starting') {
    return overlay(
      <div role="status" className="grid flex-1 place-items-center p-8 text-sm text-text-muted">
        {t('onboarding.starting')}
      </div>,
    );
  }

  if (startStatus === 'error-auth') {
    return overlay(
      <div role="alert" className="grid flex-1 place-items-center p-8 text-center">
        <p className="max-w-md text-[15.5px] text-danger">{t('onboarding.authExpired')}</p>
      </div>,
    );
  }

  if (startStatus === 'error-forbidden') {
    return overlay(
      <div role="alert" className="grid flex-1 place-items-center p-8 text-center">
        <p className="max-w-md text-[15.5px] text-danger">{t('onboarding.error.noCapability')}</p>
      </div>,
    );
  }

  if (startStatus === 'error') {
    return overlay(
      <div role="alert" className="grid flex-1 place-items-center p-8 text-center">
        <div className="flex max-w-md flex-col items-center gap-3">
          <p className="text-[15.5px] text-danger">{t('onboarding.backendUnavailable')}</p>
          <button
            type="button"
            onClick={retryStart}
            className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none transition-colors hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t('onboarding.retry')}
          </button>
        </div>
      </div>,
    );
  }

  return overlay(
    <div className="flex min-h-0 flex-1 flex-col">
      <OnboardingStepper phaseIndex={phaseIndex} />

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-2xl flex-col gap-8 px-4 py-8">
          {phase === 'credentials' ? (
            <>
              <CredentialStep
                email={email}
                password={password}
                onEmailChange={setEmail}
                onPasswordChange={setPassword}
              />
              <OnboardingWizardNav
                onBack={onClose}
                backLabel={t('onboarding.cancel')}
                onNext={() => {
                  setPhase('capabilities');
                }}
                nextLabel={t('onboarding.cta.continue')}
                nextDisabled={!canAdvanceCredentials}
              />
            </>
          ) : null}

          {phase === 'capabilities' ? (
            <>
              <CapabilityPicker
                options={capabilityOptions}
                selected={selectedCaps}
                onToggle={toggleCap}
              />
              <OnboardingWizardNav
                onBack={() => {
                  setPhase('credentials');
                }}
                backLabel={t('onboarding.back')}
                onNext={() => {
                  setPhase('interview');
                }}
                nextLabel={t('onboarding.cta.continue')}
                nextDisabled={false}
              />
            </>
          ) : null}

          {phase === 'interview' && interview !== undefined ? (
            <InterviewStep
              step={interview}
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
            />
          ) : null}

          {phase === 'review' ? (
            <ReviewStep
              email={email}
              capabilities={[...selectedCaps]}
              linkTelegram={linkTelegram}
              onToggleTelegram={setLinkTelegram}
              provisioning={provisioning}
              error={provisionError}
              onCreate={() => {
                void create();
              }}
            />
          ) : null}

          {phase === 'complete' ? (
            <div className="flex flex-col gap-8">
              <div className="flex flex-col items-center gap-3 text-center">
                <h2 className="font-display text-[22px] font-semibold text-text">
                  {t('onboarding.complete.heading')}
                </h2>
                <p className="max-w-md text-[15.5px] leading-relaxed text-text-muted">
                  {t('onboarding.complete.body')}
                </p>
              </div>

              {linkTelegram ? (
                <TelegramLinkStep
                  sessionToken={sessionToken}
                  deepLink={provisionResult?.deepLink}
                  qrSvg={provisionResult?.qrSvg}
                  polling
                />
              ) : null}

              <button
                type="button"
                onClick={onClose}
                className="min-h-[44px] self-center rounded-md border border-border bg-surface-2 px-6 py-2 text-[13px] font-semibold text-text outline-none transition-colors hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
              >
                {t('onboarding.complete.done')}
              </button>
            </div>
          ) : null}
        </div>
      </div>
    </div>,
  );
}

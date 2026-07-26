import { useCallback, useEffect, useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { CapabilityPicker } from './CapabilityPicker';
import { CredentialStep } from './CredentialStep';
import { OnboardingCenteredState, OnboardingDialog } from './OnboardingDialog';
import { OnboardingStepper } from './OnboardingStepper';
import { OnboardingWizardNav } from './OnboardingWizardNav';
import { ReviewStep } from './ReviewStep';
import { SeedProfileForm } from './SeedProfileForm';
import { TelegramLinkStep } from './TelegramLinkStep';
import {
  credentialsValid,
  isAuthError,
  isForbiddenError,
  phaseIndex as phaseIndexOf,
  provisionErrorKind,
  seedValid,
  type Phase,
  type ProvisionErrorKind,
} from './onboardingWizardModel';
import {
  provisionOnboarding,
  startOnboarding,
  type OnboardingProvisionResponse,
  type OnboardingSeed,
  type OnboardingStart,
} from './onboardingApi';
import { Button } from '@/components/ui/button';

// OnboardingWizard is the lazy default export the AppShell mounts as a FULL-SCREEN overlay (D-04 —
// NOT a governance tab, NOT a MODES entry). It runs the linear flow over the Plan-05 endpoints:
//   credentials (+ the Amendment-#95 seed form) → capabilities → review → create (/provision)
//   → Telegram link (deep-link + QR + /telegram-status poll) + completion.
// The seed rides along in the /provision body so the NEW identity's graph is seeded at creation;
// the fields are optional, and an empty seed provisions an identity that gets its own first-run
// form on first login.
// It holds the sessionToken from /start and the accumulated inputs (email/password/recovery/
// capabilities/seed), and reuses the GraphExplorer ViewStatus + error-auth contract: a /start that
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
  const [confirmPassword, setConfirmPassword] = useState('');
  const [securityQuestion, setSecurityQuestion] = useState('');
  const [securityAnswer, setSecurityAnswer] = useState('');
  const [capabilityOptions, setCapabilityOptions] = useState<readonly string[]>([]);
  const [selectedCaps, setSelectedCaps] = useState<ReadonlySet<string>>(new Set());

  const [seed, setSeed] = useState<OnboardingSeed>({});

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

  const create = useCallback(async () => {
    if (sessionToken === '') return;
    setProvisioning(true);
    setProvisionError(undefined);
    try {
      const result = await provisionOnboarding(sessionToken, {
        email,
        password,
        securityQuestion,
        securityAnswer,
        capabilities: [...selectedCaps],
        linkTelegram: true,
        seed,
      });
      setProvisionResult(result);
      // Password lives only until the saga ran; clear it so it cannot linger in state.
      setPassword('');
      setConfirmPassword('');
      setSecurityAnswer('');
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
  }, [sessionToken, email, password, securityQuestion, securityAnswer, selectedCaps, seed]);

  const canAdvanceCredentials =
    credentialsValid(email, password, confirmPassword, securityQuestion, securityAnswer) &&
    seedValid(seed);
  const phaseIndex = phaseIndexOf(phase);

  const overlay = (children: ReactNode) => (
    <OnboardingDialog
      titleId={titleId}
      title={t('onboarding.title')}
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

  if (startStatus === 'error-forbidden') {
    return overlay(
      <OnboardingCenteredState role="alert" message={t('onboarding.error.noCapability')} />,
    );
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
                confirmPassword={confirmPassword}
                securityQuestion={securityQuestion}
                securityAnswer={securityAnswer}
                onEmailChange={setEmail}
                onPasswordChange={setPassword}
                onConfirmPasswordChange={setConfirmPassword}
                onSecurityQuestionChange={setSecurityQuestion}
                onSecurityAnswerChange={setSecurityAnswer}
              />
              <SeedProfileForm value={seed} onChange={setSeed} />
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
                  setPhase('review');
                }}
                nextLabel={t('onboarding.cta.continue')}
                nextDisabled={false}
              />
            </>
          ) : null}

          {phase === 'review' ? (
            <ReviewStep
              email={email}
              capabilities={[...selectedCaps]}
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

              <TelegramLinkStep
                sessionToken={sessionToken}
                deepLink={provisionResult?.deepLink}
                qrSvg={provisionResult?.qrSvg}
                polling
              />

              <Button
                type="button"
                variant="outline"
                onClick={onClose}
                className="self-center px-6"
              >
                {t('onboarding.complete.done')}
              </Button>
            </div>
          ) : null}
        </div>
      </div>
    </div>,
  );
}

import { useCallback, useId, useState, type ReactNode } from 'react';
import { CheckCircle2, Circle, Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useCapabilities } from '../admin/useAdmin';
import { OnboardingCenteredState, OnboardingDialog } from './OnboardingDialog';
import { RouteStep } from './RouteStep';
import { SeedProfileForm } from './SeedProfileForm';
import { TelegramLinkStep } from './TelegramLinkStep';
import { TelegramTokenStep } from './TelegramTokenStep';
import { isAuthError, seedValid } from './onboardingWizardModel';
import {
  submitOnboardingProfile,
  type OnboardingProfileComplete,
  type OnboardingSeed,
} from './onboardingApi';
import { Button } from '@/components/ui/button';

// FirstRunSetup (Amendment #95) is what a signed-in operator sees on first access. It replaced
// ProfileOnboardingWizard, whose 5-question LLM interview is gone: there is no /start, no
// accumulated server session and no per-answer round-trip here, so the surface mounts straight
// into the form.
//
// Up to three steps, then ONE POST:
//   1. seed form — the typed profile fields (optional; blank = the server derives a skip)
//   2. route     — admins only: OpenRouter with the management key, or a local server. While the
//                  daemon reports routeRequired there is no Skip and no Close, because no
//                  identity's key can be minted until an admin does this.
//   3. Telegram  — the bot-token step, rehomed here from the deleted wizard
//   → submitOnboardingProfile(seed) → the completion screen with the deep-link + QR.
//
// An admin whose profile is done but whose route is required gets the route step alone, and the
// setup closes once it is done: there is no profile to submit.
//
// The submission is LAST on purpose: the server mints the Telegram deep-link BEFORE it writes
// the profile and resolves the bot name out of the settings store, so submitting after the token
// is saved is what makes the completion screen show a working link. It is a preference, not a
// precondition — the server writes the profile (or the skip sentinel) even when no link can be
// minted, and every step but a required route, Telegram included, is skippable.

type SetupStep = 'profile' | 'route' | 'telegram';
type CompletionStatus = 'idle' | 'saving' | 'completed' | 'skipped' | 'error';

// The seed form reuses the surviving `identity` copy slot; route + telegram keep their own.
const STEP_COPY_KEY: Record<SetupStep, string> = {
  profile: 'identity',
  route: 'route',
  telegram: 'telegram',
};

// setupSteps is the setup this operator gets: the profile and Telegram steps while the profile is
// still owed, and the route step for an admin, or whenever the daemon says it is required.
function setupSteps(profileRequired: boolean, showRoute: boolean): readonly SetupStep[] {
  const steps: SetupStep[] = [];
  if (profileRequired) steps.push('profile');
  if (showRoute) steps.push('route');
  if (profileRequired) steps.push('telegram');
  return steps;
}

export interface FirstRunSetupProps {
  readonly onClose: () => void;
  /** GET /api/onboarding/status `required`: the profile form and the Telegram step are owed. */
  readonly profileRequired?: boolean;
  /** GET /api/onboarding/status `routeRequired`. */
  readonly routeRequired?: boolean;
}

function ProfileProgress({
  steps,
  activeIndex,
  t,
}: {
  readonly steps: readonly SetupStep[];
  readonly activeIndex: number;
  readonly t: TFunction;
}) {
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
        {steps.map((step, index) => {
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
                  {t(`onboarding.profile.steps.${STEP_COPY_KEY[step]}.label`)}
                </span>
                <span className="block text-[13px] leading-relaxed text-text-muted">
                  {t(`onboarding.profile.steps.${STEP_COPY_KEY[step]}.help`)}
                </span>
              </span>
            </li>
          );
        })}
      </ol>
    </aside>
  );
}

export default function FirstRunSetup({
  onClose,
  profileRequired = true,
  routeRequired: routeRequiredAtOpen = false,
}: FirstRunSetupProps) {
  const { t } = useTranslation();
  const titleId = useId();
  const { isAdmin } = useCapabilities();
  const steps = setupSteps(profileRequired, isAdmin || routeRequiredAtOpen);

  const [step, setStep] = useState<SetupStep>(steps[0] ?? 'profile');
  const [routeRequired, setRouteRequired] = useState(routeRequiredAtOpen);
  const [seed, setSeed] = useState<OnboardingSeed>({});
  const [authExpired, setAuthExpired] = useState(false);
  const [completionStatus, setCompletionStatus] = useState<CompletionStatus>('idle');
  const [completionResult, setCompletionResult] = useState<OnboardingProfileComplete | undefined>(
    undefined,
  );

  const submit = useCallback(async (submitted: OnboardingSeed) => {
    setCompletionStatus('saving');
    try {
      const done = await submitOnboardingProfile(submitted);
      setCompletionResult(done);
      setCompletionStatus(done.skipped ? 'skipped' : 'completed');
    } catch (err) {
      if (isAuthError(err)) {
        setAuthExpired(true);
        return;
      }
      setCompletionStatus('error');
    }
  }, []);

  const stepAfter = (current: SetupStep): SetupStep | undefined =>
    steps[steps.indexOf(current) + 1];

  // Skipping does NOT submit immediately: it drops the seed and walks on, so the empty
  // submission still happens AFTER the bot token exists and the completion screen can still
  // offer a link. The server derives "skipped" from the blank seed.
  const skipProfile = () => {
    setSeed({});
    setStep(stepAfter('profile') ?? 'telegram');
  };

  const finishRoute = () => {
    const next = stepAfter('route');
    if (next === undefined) {
      onClose();
      return;
    }
    setStep(next);
  };

  const activeIndex = steps.indexOf(step);

  const overlay = (children: ReactNode) => (
    <OnboardingDialog
      titleId={titleId}
      kicker={t('onboarding.profile.kicker')}
      title={t('onboarding.profile.title')}
      closeLabel={t('onboarding.close')}
      onClose={routeRequired ? undefined : onClose}
    >
      {children}
    </OnboardingDialog>
  );

  if (authExpired) {
    return overlay(<OnboardingCenteredState role="alert" message={t('onboarding.authExpired')} />);
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
            sessionToken={completionResult?.sessionToken ?? ''}
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
      <ProfileProgress steps={steps} activeIndex={activeIndex} t={t} />
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="border-b border-border bg-surface px-4 py-4 lg:hidden">
          <p className="text-sm font-semibold text-text">
            {t('onboarding.profile.currentStep', {
              current: activeIndex + 1,
              total: steps.length,
            })}
          </p>
          <div className="mt-3 h-2 overflow-hidden rounded-full bg-surface-2">
            <div
              className="h-full rounded-full bg-accent"
              style={{
                width: `${String(Math.round(((activeIndex + 1) / steps.length) * 100))}%`,
              }}
            />
          </div>
        </div>
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-7 px-4 py-6 sm:px-6 lg:px-10 lg:py-10">
          <section className="flex flex-col gap-2">
            <p className="hidden text-sm font-semibold text-accent-text lg:block">
              {t('onboarding.profile.currentStep', {
                current: activeIndex + 1,
                total: steps.length,
              })}
            </p>
            {step === 'profile' ? null : (
              <>
                <h2 className="font-display text-[24px] font-semibold text-text">
                  {t(`onboarding.profile.steps.${STEP_COPY_KEY[step]}.label`)}
                </h2>
                <p className="max-w-2xl text-[15.5px] leading-relaxed text-text-muted">
                  {t(`onboarding.profile.steps.${STEP_COPY_KEY[step]}.help`)}
                </p>
              </>
            )}
          </section>

          {completionStatus === 'error' ? (
            <div role="alert" className="flex flex-wrap items-center justify-between gap-3">
              <p className="text-[15.5px] text-danger">{t('onboarding.profile.saveError')}</p>
              <Button type="button" variant="outline" onClick={() => void submit(seed)}>
                {t('onboarding.retry')}
              </Button>
            </div>
          ) : null}

          {step === 'profile' ? (
            <>
              <SeedProfileForm value={seed} onChange={setSeed} />
              <div className="flex flex-wrap items-center gap-3">
                <Button
                  type="button"
                  disabled={!seedValid(seed)}
                  onClick={() => {
                    setStep(stepAfter('profile') ?? 'telegram');
                  }}
                  className="px-6"
                >
                  {t('onboarding.cta.continue')}
                </Button>
                <Button type="button" variant="ghost" onClick={skipProfile}>
                  {t('onboarding.profile.skipSetup')}
                </Button>
              </div>
            </>
          ) : null}

          {step === 'route' ? (
            <RouteStep
              required={routeRequired}
              onRequiredChange={setRouteRequired}
              onDone={finishRoute}
            />
          ) : null}

          {step === 'telegram' ? (
            <>
              <TelegramTokenStep onDone={() => void submit(seed)} />
              {/* The submission is reachable WITHOUT a bot token. TelegramTokenStep's own only
                  control is a Verify button that stays disabled until a token is typed, so on an
                  instance with no @BotFather token this is the sole path off the last step — and
                  without it a skip would never write its sentinel and first-run setup would
                  re-open on every login forever. */}
              <div className="flex flex-wrap items-center gap-3">
                <Button type="button" variant="ghost" onClick={() => void submit(seed)}>
                  {t('onboarding.profile.telegram.skip')}
                </Button>
              </div>
            </>
          ) : null}
        </div>
      </main>
    </div>,
  );
}

import { useTranslation } from 'react-i18next';
import type { ProvisionErrorKind } from './onboardingWizardModel';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

// ReviewStep (ONBD-01a / UI-SPEC) — the final summary before the cross-store saga. It shows the
// new operator email, what access the identity will have, what credit it starts with, and the
// required Telegram-link posture, then a CONSTRUCTIVE "Create identity" CTA (the reserved accent
// — NOT a danger-styled confirm: identity creation is a constructive mutation, UI-SPEC
// §Destructive confirmation note). The CTA calls /provision via the wizard. While the saga runs
// it shows "Creating identity…"; the three distinct failure paths render distinct copy
// (T-28-06-04):
//   - 403 → no-permission   - 409 → duplicate/empty email   - rolled-back (502/other) → nothing saved
// The password is NEVER echoed here (no-leak, T-28-06-01).
//
// The "Granted capabilities" badge list is gone with the picker that fed it: RBAC-03 grants every
// provisioned identity exactly identity.UserSet(), so there was never a per-identity answer to
// show. The two rows that replace it are FIXED prose the wizard already knows before the identity
// exists — no interaction, no checkbox, no server round-trip.
//
// "Starting credit: $0.00" is the deliberate exception to CRED-09's never-render-a-zero-balance
// rule. That rule is about a backend which does not bill; this is a stated starting CONDITION
// with its consequence attached, and the sentence is what makes it one. The number alone would be
// the misleading form, so the copy keeps both halves together.

export interface ReviewStepProps {
  readonly email: string;
  readonly provisioning: boolean;
  readonly error: ProvisionErrorKind | undefined;
  readonly onCreate: () => void;
}

export function ReviewStep({ email, provisioning, error, onCreate }: ReviewStepProps) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-display text-[22px] font-semibold text-text">
        {t('onboarding.review.heading')}
      </h2>

      <dl className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <dt className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('onboarding.review.emailLabel')}
          </dt>
          {/* Email is an identifier-shaped value — mono, React-escaped. */}
          <dd className="break-all font-mono text-[15.5px] text-text">{email}</dd>
        </div>

        <div className="flex flex-col gap-1">
          <dt className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('admin.reviewStep.accessLabel')}
          </dt>
          <dd className="text-[15.5px] leading-relaxed text-text">
            {t('admin.reviewStep.accessBody')}
          </dd>
        </div>

        <div className="flex flex-col gap-1">
          <dt className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('admin.reviewStep.creditLabel')}
          </dt>
          <dd className="text-[15.5px] leading-relaxed text-text">
            {t('admin.reviewStep.creditBody')}
          </dd>
        </div>

        <div className="flex flex-col gap-1">
          <dt className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('onboarding.review.telegramLabel')}
          </dt>
          <dd className="text-[15.5px] text-text">{t('onboarding.review.telegramRequired')}</dd>
        </div>
      </dl>

      {error !== undefined ? (
        <Alert variant="destructive">
          <AlertDescription>{t(`onboarding.error.${error}`)}</AlertDescription>
        </Alert>
      ) : null}

      {/* CONSTRUCTIVE primary CTA — reserved accent, NOT danger-styled. */}
      <Button
        type="button"
        disabled={provisioning}
        aria-busy={provisioning}
        onClick={onCreate}
        className="text-[15.5px]"
      >
        {provisioning ? t('onboarding.cta.provisionInFlight') : t('onboarding.cta.provision')}
      </Button>
    </div>
  );
}

import { useTranslation } from 'react-i18next';
import type { ProvisionErrorKind } from './onboardingWizardModel';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

// ReviewStep (ONBD-01a / UI-SPEC) — the final summary before the cross-store saga. It shows the
// new operator email + the chosen capabilities + the required Telegram-link posture, and a CONSTRUCTIVE
// "Create identity" CTA (the reserved accent — NOT a danger-styled confirm: identity creation is a
// constructive mutation, UI-SPEC §Destructive confirmation note). The CTA calls /provision via the
// wizard. While the saga runs it shows "Creating identity…"; the three distinct failure paths
// render distinct copy (T-28-06-04):
//   - 403 → no-permission   - 409 → duplicate/empty email   - rolled-back (502/other) → nothing saved
// The password is NEVER echoed here (no-leak, T-28-06-01) — only the email + capabilities + the
// Telegram requirement. Capability names are backend-supplied → React-escaped mono text.

export interface ReviewStepProps {
  readonly email: string;
  readonly capabilities: readonly string[];
  readonly provisioning: boolean;
  readonly error: ProvisionErrorKind | undefined;
  readonly onCreate: () => void;
}

export function ReviewStep({
  email,
  capabilities,
  provisioning,
  error,
  onCreate,
}: ReviewStepProps) {
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
            {t('onboarding.review.capabilitiesLabel')}
          </dt>
          <dd className="text-[15.5px] text-text">
            {capabilities.length === 0 ? (
              <span className="text-text-muted">{t('onboarding.review.noCapabilities')}</span>
            ) : (
              <ul className="flex flex-wrap gap-2">
                {capabilities.map((c) => (
                  <li key={c}>
                    <Badge variant="secondary" className="font-mono text-[13px] text-text">
                      {c}
                    </Badge>
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </div>

        <div className="flex flex-col gap-1">
          <dt className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('onboarding.review.telegramLabel')}
          </dt>
          <dd className="text-[15.5px] text-text">
            {t('onboarding.review.telegramRequired')}
          </dd>
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

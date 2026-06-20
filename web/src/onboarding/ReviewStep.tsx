import { useId } from 'react';
import { useTranslation } from 'react-i18next';

// ReviewStep (ONBD-01a / UI-SPEC) — the final summary before the cross-store saga. It shows the
// new operator email + the chosen capabilities + the Telegram-link choice, and a CONSTRUCTIVE
// "Create identity" CTA (the reserved accent — NOT a danger-styled confirm: identity creation is a
// constructive mutation, UI-SPEC §Destructive confirmation note). The CTA calls /provision via the
// wizard. While the saga runs it shows "Creating identity…"; the three distinct failure paths
// render distinct copy (T-28-06-04):
//   - 403 → no-permission   - 409 → duplicate/empty email   - rolled-back (502/other) → nothing saved
// The password is NEVER echoed here (no-leak, T-28-06-01) — only the email + capabilities + the
// Telegram choice. Capability names are backend-supplied → React-escaped mono text.

/** The mapped provision-error copy key, or undefined when there is no error. */
export type ProvisionErrorKind = 'noCapability' | 'duplicate' | 'rolledBack';

export interface ReviewStepProps {
  readonly email: string;
  readonly capabilities: readonly string[];
  readonly linkTelegram: boolean;
  readonly onToggleTelegram: (value: boolean) => void;
  readonly provisioning: boolean;
  readonly error: ProvisionErrorKind | undefined;
  readonly onCreate: () => void;
}

export function ReviewStep({
  email,
  capabilities,
  linkTelegram,
  onToggleTelegram,
  provisioning,
  error,
  onCreate,
}: ReviewStepProps) {
  const { t } = useTranslation();
  const telegramId = useId();

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
                  <li
                    key={c}
                    className="rounded-sm bg-surface-3 px-2 py-0.5 font-mono text-[13px] text-text"
                  >
                    {c}
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
          <dd>
            <label
              htmlFor={telegramId}
              className="flex min-h-[44px] cursor-pointer items-center gap-3 text-[15.5px] text-text"
            >
              <input
                id={telegramId}
                type="checkbox"
                checked={linkTelegram}
                onChange={(e) => {
                  onToggleTelegram(e.target.checked);
                }}
                className="h-5 w-5 shrink-0 accent-[var(--color-accent-text)] outline-none"
              />
              <span>
                {linkTelegram
                  ? t('onboarding.review.telegramOn')
                  : t('onboarding.review.telegramOff')}
              </span>
            </label>
          </dd>
        </div>
      </dl>

      {error !== undefined ? (
        <p role="alert" className="text-[15.5px] text-danger">
          {t(`onboarding.error.${error}`)}
        </p>
      ) : null}

      {/* CONSTRUCTIVE primary CTA — reserved accent, NOT danger-styled. */}
      <button
        type="button"
        disabled={provisioning}
        aria-busy={provisioning}
        onClick={onCreate}
        className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[15.5px] font-semibold text-on-accent outline-none transition-opacity hover:opacity-90 focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-wait disabled:opacity-60"
      >
        {provisioning ? t('onboarding.cta.provisionInFlight') : t('onboarding.cta.provision')}
      </button>
    </div>
  );
}

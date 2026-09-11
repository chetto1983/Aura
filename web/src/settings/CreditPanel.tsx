import { useState } from 'react';
import { KeyRound, Wallet } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useIdentityCredit, useSetIdentityCredit } from '../admin/useAdmin';
import {
  CONTEXT_CRITICAL_PERCENT,
  CONTEXT_NEAR_FULL_PERCENT,
  gaugeTier,
} from '../chat/footerMetrics';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';

// CreditPanel (CRED-03/CRED-06/CRED-09) is the per-identity half of the cockpit's money
// surface: the cap the admin sets, the reset interval it rides, and the spend measured against
// it. Every figure comes from GET /api/admin/identities/{id}/credit, which reads Aura's own
// in-band ledger (D-08) -- never GET /api/v1/key's usage counter, which lags a spend by 30-40s
// (M-07) and is therefore wrong at exactly the moment a human reads it.
//
// The gauge is ContextBudgetGauge's shape reapplied to a USD cap: the same role="progressbar"
// + aria-valuenow pair carrying the meaning (colour is decorative, WCAG 1.4.1) and the same
// three tiers, imported BY NAME from footerMetrics rather than re-declared, so the credit gauge
// and the context gauge can never drift to different thresholds.

/** The three reset intervals credit_api.go's validLimitReset accepts. No fourth "never" option:
 * OpenRouter's `limit_reset: null` exists on the wire, but D-10 frames this as the admin
 * choosing among three intervals, so offering a fourth would invent a choice nobody asked for. */
const RESET_INTERVALS = ['daily', 'weekly', 'monthly'] as const;
type ResetInterval = (typeof RESET_INTERVALS)[number];

const DEFAULT_RESET: ResetInterval = 'monthly';

/** The reasons credit_api.go's noKeyCause gives for an identity with no key. A code this build
 * does not know reads as not minted yet, the one cause that asks nothing of the admin. */
const NO_KEY_CAUSES = ['management_key_unset', 'minting_unavailable', 'not_minted'] as const;

function noKeyCauseKey(cause: string): string {
  const known = (NO_KEY_CAUSES as readonly string[]).includes(cause) ? cause : 'not_minted';
  return `admin.credit.noKeyCause.${known}`;
}

function asResetInterval(value: string): ResetInterval {
  return (RESET_INTERVALS as readonly string[]).includes(value)
    ? (value as ResetInterval)
    : DEFAULT_RESET;
}

/**
 * formatUsd renders a ledger-space amount without collapsing it.
 *
 * A cap is cap-space and always two decimals. A SPEND is not: migration 0124 widened
 * aura.cache_metrics.cost_usd to numeric(24,12) precisely because a measured per-call cost is
 * 0.000004158 (02-CONTEXT M-09), and commit 61c30e4a6 stopped the server rounding it to cents.
 * A client that then rendered it at two decimals would reintroduce the same defect one layer
 * up, so a sub-cent amount grows decimals until its leading significant digit is visible.
 *
 * A genuine zero on a BILLING backend renders as two decimals like any other amount. That is
 * not the zero CRED-09 forbids -- that one is a non-billing deployment, which never reaches
 * this function because the exempt branch replaces the whole panel.
 */
function formatUsd(amount: number): string {
  const value = Number.isFinite(amount) && amount > 0 ? amount : 0;
  if (value === 0 || value >= 0.01) return `$${value.toFixed(2)}`;
  const digits = Math.min(12, 2 - Math.floor(Math.log10(value)));
  return `$${trimZeros(value.toFixed(digits))}`;
}

/** Drop trailing zeros the widened decimal count added, never below two places. */
function trimZeros(fixed: string): string {
  const trimmed = fixed.replace(/0+$/, '');
  const [whole, frac = ''] = trimmed.split('.');
  return frac.length >= 2 ? trimmed : `${whole ?? ''}.${frac.padEnd(2, '0')}`;
}

/** clampPercent takes the server's own percent_used rather than re-deriving it. credit_api.go
 * decides exhaustion by COMPARISON (spend >= cap is 100, everything else caps at 99) because a
 * float ratio at the boundary can land a hair under and report an exhausted identity as 99%.
 * Re-computing the ratio here would be a second rule to keep in sync with that one. */
function clampPercent(percent: number): number {
  if (!Number.isFinite(percent)) return 0;
  return Math.min(100, Math.max(0, Math.round(percent)));
}

/** The Empty composition the panel shows in place of a cap: a backend that bills nothing, or an
 * identity with no key yet. */
function CreditEmpty({
  icon: Icon,
  heading,
  body,
}: {
  readonly icon: typeof Wallet;
  readonly heading: string;
  readonly body: string;
}) {
  return (
    <Empty className="border border-dashed border-border bg-surface-2/40 py-8">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon aria-hidden="true" className="size-5" />
        </EmptyMedia>
        <EmptyTitle className="text-sm">{heading}</EmptyTitle>
        <EmptyDescription>{body}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

export interface CreditPanelProps {
  readonly identityId: string;
}

export function CreditPanel({ identityId }: CreditPanelProps) {
  const { t } = useTranslation();
  const creditQuery = useIdentityCredit(identityId);
  const saveMutation = useSetIdentityCredit();

  const credit = creditQuery.data;

  // The two form fields are UNDEFINED until the admin touches them, and fall back to the
  // server's own figures for their displayed value. Seeding them from an effect instead would
  // be a setState cascade on every refetch (react-compiler set-state-in-effect) AND would make
  // the field the source of truth for a number the server owns.
  const [capEdit, setCapEdit] = useState<string | undefined>(undefined);
  const [resetEdit, setResetEdit] = useState<ResetInterval | undefined>(undefined);
  const [advisory, setAdvisory] = useState<'up' | 'down' | undefined>(undefined);

  if (creditQuery.isLoading) {
    return (
      <div role="status" className="flex items-center gap-2 text-sm text-text-muted">
        <Spinner />
        {t('admin.credit.loading')}
      </div>
    );
  }

  if (creditQuery.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{t('admin.credit.loadError')}</AlertDescription>
      </Alert>
    );
  }

  // A missing key is a state of the identity, not a failed read: the daemon says why, and the
  // admin is told what, if anything, to do about it.
  if (credit !== undefined && 'no_key' in credit) {
    return (
      <CreditEmpty
        icon={KeyRound}
        heading={t('admin.credit.noKeyHeading')}
        body={t(noKeyCauseKey(credit.cause))}
      />
    );
  }

  // CRED-09: a deployment whose backend does not bill is told it is EXEMPT. It is never shown a
  // zero balance -- an exemption and an exhaustion are different facts, and rendering the first
  // as the second is the LibreChat `tokenCredits.toFixed(2)` shape the UI-SPEC rejected by name.
  if (credit === undefined || credit.exempt) {
    return (
      <CreditEmpty
        icon={Wallet}
        heading={t('admin.credit.emptyHeading')}
        body={t('admin.credit.emptyBody')}
      />
    );
  }

  // An administrator's own key has no spending limit, and the reconciler clears any cap put on
  // it, so there is no cap to edit and no gauge to fill: the panel says so and shows the spend.
  if (credit.unlimited === true) {
    return (
      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-semibold uppercase tracking-wide text-text-faint">
          {t('admin.credit.capLabel')}
        </span>
        <p className="text-sm text-text">{t('admin.credit.noLimit')}</p>
        <p className="text-[13px] text-text-muted">{t('admin.credit.noLimitBody')}</p>
        <p className="font-mono text-[12px] text-text-muted">
          {t('admin.credit.spendLabel')}: {formatUsd(credit.spend)}
        </p>
      </div>
    );
  }

  const previousCap = credit.cap;
  const capValue = capEdit ?? credit.cap.toFixed(2);
  const resetValue = resetEdit ?? asResetInterval(credit.reset_interval);
  const percent = clampPercent(credit.percent_used);
  const tier = gaugeTier(percent);
  const fillClass =
    tier === 'critical' ? 'bg-danger' : tier === 'near' ? 'bg-warning' : 'bg-accent';
  const capId = `credit-cap-${identityId}`;
  const resetId = `credit-reset-${identityId}`;

  function save() {
    saveMutation.mutate(
      { identityId, patch: { cap: capValue, reset_interval: resetValue } },
      {
        onSuccess: (result) => {
          // Only a clear_cap answers without a cap, and this form always sends a cap.
          if (result.cap === null) return;
          // Both the field and the advisory read the APPLIED cap, not the typed one: 5.126 and
          // 5.13 are the same change, and the admin must be shown the figure the store and the
          // provider actually hold rather than the one they happened to type.
          setCapEdit(result.cap.toFixed(2));
          setResetEdit(asResetInterval(result.reset_interval));
          setAdvisory(
            result.cap > previousCap ? 'up' : result.cap < previousCap ? 'down' : undefined,
          );
        },
      },
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {/* 1. The cap, and the one accent-variant button this phase adds to the reservation. */}
      <div className="flex flex-col gap-2">
        <Label htmlFor={capId}>{t('admin.credit.capLabel')}</Label>
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-1.5">
            <span aria-hidden="true" className="font-mono text-[15.5px] text-text-muted">
              $
            </span>
            <Input
              id={capId}
              type="number"
              inputMode="decimal"
              min="0"
              step="0.01"
              value={capValue}
              onChange={(event) => {
                setCapEdit(event.target.value);
              }}
              className="w-32 font-mono text-[13px]"
            />
          </div>
          <Button
            type="button"
            disabled={saveMutation.isPending}
            aria-busy={saveMutation.isPending}
            onClick={save}
          >
            {saveMutation.isPending ? <Spinner /> : null}
            {t('admin.credit.saveCap')}
          </Button>
        </div>
      </div>

      {/* 2. The reset interval. */}
      <div className="flex flex-col gap-2">
        <Label htmlFor={resetId}>{t('admin.credit.resetLabel')}</Label>
        <NativeSelect
          id={resetId}
          value={resetValue}
          onChange={(event) => {
            setResetEdit(asResetInterval(event.target.value));
          }}
        >
          {RESET_INTERVALS.map((interval) => (
            <NativeSelectOption key={interval} value={interval}>
              {t(`admin.credit.interval.${interval}`)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>

      {/* 3. Spend against the cap, plus remaining. Both ledger figures, both mono. */}
      <div className="flex flex-col gap-1.5">
        <div className="flex items-baseline justify-between gap-2">
          <span className="text-xs font-semibold uppercase tracking-wide text-text-faint">
            {t('admin.credit.spendLabel')}
          </span>
          <span className="font-mono text-[13px] text-text">
            {t('admin.credit.gaugeValue', {
              spend: formatUsd(credit.spend),
              cap: formatUsd(credit.cap),
              percent,
            })}
          </span>
        </div>
        <div
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
          aria-label={t('admin.credit.gaugeLabel', {
            near: CONTEXT_NEAR_FULL_PERCENT,
            critical: CONTEXT_CRITICAL_PERCENT,
          })}
          className="h-1.5 w-full overflow-hidden rounded-full bg-surface-2"
        >
          <div
            className={`h-full rounded-full transition-[width] motion-reduce:transition-none ${fillClass}`}
            style={{ width: `${String(percent)}%` }}
          />
        </div>
        <p className="font-mono text-[12px] text-text-muted">
          {t('admin.credit.remainingLabel')}: {formatUsd(credit.remaining)}
        </p>
      </div>

      {/* 4. The direction-dependent latency advisory. Two strings, because the two measured
          latencies differ by 5x (M-06 raising, M-05 lowering) and one generic "a moment" would
          make the slower of them look broken. */}
      {advisory !== undefined && !saveMutation.isPending ? (
        <p className="text-[13px] text-warning">
          {t(advisory === 'up' ? 'admin.credit.latencyUp' : 'admin.credit.latencyDown')}
        </p>
      ) : null}

      {saveMutation.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('admin.credit.saveError')}</AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}

import { useTranslation } from 'react-i18next';
import { hasCapability, IDENTITY_CREATE } from '../admin/adminApi';
import { useAdminIdentities, useSpendOverview } from '../admin/useAdmin';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';

// SpendOverview (RBAC-11/CRED-06, plan 02-09) is the account-wide reconciliation dashboard
// the UI-SPEC's §Admin spend dashboard — Overview specifies: five KPI tiles with
// sparklines and prior-period deltas, a Top-Identities-by-spend ranked list, and the
// over-allocation advisory. Mounted above the Roster inside IdentityAccessPanel.tsx, same
// admin-gated `identities` settings section.
//
// Additive, not a replacement: this is reconciliation tier (D-08) — the figures here come
// from OpenRouter's own key roster + analytics, refreshed periodically, and NEVER the
// number CRED-05's pre-flight refusal or the Credit panel's CRED-06 read use, which stay on
// Aura's in-band ledger. The two legitimately disagree during the provider's measured
// 30-40s lag (M-07) — that is by design, not a bug.
//
// No charting library — matching the zero-dependency convention
// web/src/chat/displays/ChartDisplay.tsx already established. Sparklines are inline SVG
// <polyline> elements; the current-period trace uses --color-info (the app's accent
// reservation is closed at exactly two items this phase, and a sparkline is not a third).
//
// Scope fence: no tab bar, and no chrome for OpenRouter's three other dashboard tabs
// (named in 02-UI-SPEC.md's own Scope fence, deliberately not repeated here — this file's
// own verify command greps for those names to prove none of them is rendered, even inert).
// They are explicitly out of scope this phase; rendering them inert would be exactly the
// "asilo nido" CLAUDE.md forbids.

/** The five KPI metrics, in the order the UI-SPEC's KPI row specifies. */
const KPI_METRIC_ORDER = [
  'total_usage',
  'request_count',
  'tokens_total',
  'cache_hit_rate',
  'blended_cost_per_million_tokens',
] as const;

const KPI_LABEL_KEY: Record<string, string> = {
  total_usage: 'admin.overview.kpi.totalSpend',
  request_count: 'admin.overview.kpi.requests',
  tokens_total: 'admin.overview.kpi.tokenVolume',
  cache_hit_rate: 'admin.overview.kpi.cacheHitRate',
  blended_cost_per_million_tokens: 'admin.overview.kpi.blendedCost',
};

/** Which metrics render as USD (font-mono, $-prefixed, auto-compact above $1,000). */
const DOLLAR_METRICS = new Set(['total_usage', 'blended_cost_per_million_tokens']);
/** Which metrics render as a compact plain count (requests, tokens). */
const COUNT_METRICS = new Set(['request_count', 'tokens_total']);
/** cache_hit_rate is the only percent-shaped metric in this row. */
const PERCENT_METRICS = new Set(['cache_hit_rate']);

/**
 * The delta-colouring rule is the one place this surface deliberately departs from
 * OpenRouter's own screenshot, which colours every "up" delta green. A rising spend figure
 * is not an achievement, so volume metrics (spend/requests/tokens) render their delta in
 * text-text-muted regardless of sign. Only the two RATE metrics carry direction-aware
 * colour, and in OPPOSITE directions from each other: cache hit rate rising is good
 * (text-success), cost-per-token rising is the one worth flagging (text-warning) — danger
 * stays reserved for the exhausted-credit/critical-cap meaning elsewhere in this app.
 */
function deltaClassName(metric: string, delta: number): string {
  if (delta === 0) return 'text-text-muted';
  const up = delta > 0;
  if (metric === 'cache_hit_rate') return up ? 'text-success' : 'text-warning';
  if (metric === 'blended_cost_per_million_tokens') return up ? 'text-warning' : 'text-success';
  return 'text-text-muted';
}

/** formatKPIValue is auto-compact: a large figure never overruns the tile ($5.52, 3K,
 * 52.1M, 51.5%, $0.11), and a small dollar figure keeps its full two decimals rather than
 * being rounded away by compact notation (which would turn $0.11 into $0.1). */
function formatKPIValue(metric: string, value: number): string {
  if (DOLLAR_METRICS.has(metric)) return formatCompactUsd(value);
  if (COUNT_METRICS.has(metric)) {
    return new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 }).format(
      value,
    );
  }
  if (PERCENT_METRICS.has(metric)) return `${(value * 100).toFixed(1)}%`;
  return String(value);
}

function formatCompactUsd(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1000) {
    return new Intl.NumberFormat('en', {
      notation: 'compact',
      style: 'currency',
      currency: 'USD',
      maximumFractionDigits: 1,
    }).format(value);
  }
  if (abs === 0 || abs >= 0.01) return `$${value.toFixed(2)}`;
  // Sub-cent: grow precision until the leading significant digit is visible, the same
  // discipline CreditPanel.tsx's formatUsd already established for a ledger-space figure.
  const digits = Math.min(12, 2 - Math.floor(Math.log10(abs)));
  return `$${value.toFixed(digits)}`;
}

function formatDelta(delta: number): string {
  const arrow = delta >= 0 ? '↑' : '↓';
  return `${arrow}${Math.abs(delta).toFixed(1)}%`;
}

/** A 12-point inline SVG sparkline — no charting library, matching ChartDisplay.tsx's own
 * zero-dependency CSS/SVG-only convention. Single series (current period), no legend box
 * (the tile's own label already names what is plotted). */
function Sparkline({ series }: { readonly series: readonly number[] }) {
  if (series.length < 2) return null;
  const width = 100;
  const height = 24;
  const max = Math.max(...series);
  const min = Math.min(...series);
  const range = max - min || 1;
  const points = series
    .map((v, i) => {
      const x = (i / (series.length - 1)) * width;
      const y = height - ((v - min) / range) * height;
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(' ');
  return (
    <svg
      viewBox={`0 0 ${String(width)} ${String(height)}`}
      className="h-6 w-full"
      aria-hidden="true"
    >
      <polyline
        points={points}
        fill="none"
        strokeWidth="1.5"
        style={{ stroke: 'var(--color-info)' }}
      />
    </svg>
  );
}

export function SpendOverview() {
  const { t } = useTranslation();
  const overviewQuery = useSpendOverview();
  // The Top-Identities role badge reuses the SAME roster data the Roster below already
  // fetches (React Query dedupes the identical queryKey) rather than duplicating role
  // derivation server-side — the ranked list IS the roster, re-sorted by spend.
  const identitiesQuery = useAdminIdentities();
  const rosterByID = new Map((identitiesQuery.data ?? []).map((idn) => [idn.id, idn]));

  if (overviewQuery.isLoading) {
    return (
      <div role="status" className="flex items-center gap-2 text-sm text-text-muted">
        <Spinner />
        {t('admin.overview.loading')}
      </div>
    );
  }

  if (overviewQuery.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{t('admin.overview.loadError')}</AlertDescription>
      </Alert>
    );
  }

  const data = overviewQuery.data;
  if (data === undefined) return null;

  const requestsTile = data.kpis.find((k) => k.metric === 'request_count');
  const isEmpty = (requestsTile?.value ?? 0) === 0;

  if (isEmpty) {
    return (
      <section aria-labelledby="settings-spend-overview" className="flex flex-col gap-2">
        <h2 id="settings-spend-overview" className="text-[20px] font-semibold text-text">
          {t('admin.overview.heading')}
        </h2>
        <p className="text-sm text-text-muted">{t('admin.overview.empty')}</p>
      </section>
    );
  }

  const orderedKPIs = KPI_METRIC_ORDER.map((metric) =>
    data.kpis.find((k) => k.metric === metric),
  ).filter((k): k is (typeof data.kpis)[number] => k !== undefined);

  return (
    <section aria-labelledby="settings-spend-overview" className="flex flex-col gap-4">
      <h2 id="settings-spend-overview" className="text-[20px] font-semibold text-text">
        {t('admin.overview.heading')}
      </h2>

      {data.over_allocation.triggered ? (
        <p className="rounded-lg border border-warning/40 bg-warning/10 p-3 text-[13px] text-warning">
          {t('admin.overview.overAllocation')}
        </p>
      ) : null}

      <div className="flex flex-wrap gap-3">
        {orderedKPIs.map((kpi) => (
          <div
            key={kpi.metric}
            className="flex min-w-[10rem] flex-1 flex-col gap-1 rounded-lg border border-border bg-surface p-4"
          >
            <span className="text-xs font-semibold uppercase tracking-wide text-text-faint">
              {t(KPI_LABEL_KEY[kpi.metric] ?? kpi.metric)}
            </span>
            <span className="font-mono text-[22px] font-semibold text-text">
              {formatKPIValue(kpi.metric, kpi.value)}
            </span>
            {kpi.delta_percent !== null ? (
              <span className={`text-[13px] ${deltaClassName(kpi.metric, kpi.delta_percent)}`}>
                {formatDelta(kpi.delta_percent)} {t('admin.overview.vsPrevPeriod')}
              </span>
            ) : null}
            <Sparkline series={kpi.series} />
          </div>
        ))}
      </div>

      <div className="flex flex-col gap-2">
        <h3 className="text-[13px] font-semibold uppercase tracking-wide text-text-faint">
          {t('admin.overview.topIdentities.heading')}
        </h3>
        <div role="list" className="flex flex-col gap-2">
          {data.top_identities.map((row) => {
            const roster = rosterByID.get(row.identity_id);
            const admin = roster !== undefined && hasCapability(roster.capabilities, IDENTITY_CREATE);
            return (
              <div
                role="listitem"
                key={row.identity_id}
                className="flex items-center justify-between gap-3 rounded-lg border border-border bg-surface p-3"
              >
                <div className="flex min-w-0 flex-col gap-0.5">
                  <div className="flex items-center gap-2">
                    <Badge variant={admin ? 'info' : 'secondary'}>
                      {admin ? t('admin.roster.adminBadge') : t('admin.roster.memberBadge')}
                    </Badge>
                    <span className="break-all font-mono text-[15.5px] text-text">{row.name}</span>
                  </div>
                  <span className="break-all font-mono text-[12px] text-text-muted">
                    {row.masked_label}
                  </span>
                </div>
                <span className="shrink-0 font-mono text-[13px] text-text">
                  {t('admin.overview.topIdentities.lifetimeSpend')}:{' '}
                  {formatKPIValue('total_usage', row.lifetime_spend)}
                </span>
              </div>
            );
          })}
        </div>
        <p className="text-[12px] text-text-muted">
          {t('admin.overview.topIdentities.seeFullRoster')}
        </p>
      </div>
    </section>
  );
}

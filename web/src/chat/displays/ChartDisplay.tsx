import { useTranslation } from 'react-i18next';
import type { DisplayChart } from './types';
import { DisplayCardShell } from './DisplayCardShell';

// ChartDisplay (D-02): a ZERO-DEPENDENCY bar chart over the swap-ready
// `{x_labels, y_values, x_axis_label}` payload. It renders CSS-width bars (no SVG
// canvas math, no charting library — never recharts; uPlot is the future swap if a
// real numeric series source lands) PLUS an accessible <table> fallback carrying the
// exact data behind the bars, so a screen reader / no-CSS context still reads the
// series. The bars are decorative (aria-hidden); the <table> is the source of truth.
//
// SECURITY/contract: labels render as React-escaped text; values are coerced through
// Number formatting — no injection surface.

export interface ChartDisplayProps {
  readonly payload: { readonly chart?: DisplayChart };
}

/** Pair labels with values, tolerating a length mismatch by zipping to the shorter. */
function zip(labels: readonly string[], values: readonly number[]) {
  const n = Math.min(labels.length, values.length);
  const pairs: { label: string; value: number }[] = [];
  for (let i = 0; i < n; i++) {
    pairs.push({ label: labels[i] ?? '', value: values[i] ?? 0 });
  }
  return pairs;
}

export function ChartDisplay({ payload }: ChartDisplayProps) {
  const { t } = useTranslation();
  const chart = payload.chart;
  const label = t('display.type.chart');

  const pairs = chart ? zip(chart.x_labels, chart.y_values) : [];

  if (pairs.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <div className="flex flex-col items-center gap-1 py-8 text-center">
          <p className="text-sm font-medium text-text">{t('display.chart.emptyHeading')}</p>
          <p className="text-[0.75rem] text-text-faint">{t('display.chart.emptyBody')}</p>
        </div>
      </DisplayCardShell>
    );
  }

  // Bars are scaled against the max magnitude so a single dominant value doesn't
  // flatten the rest; a non-positive max degrades to a flat baseline (no divide-by-0).
  const max = Math.max(...pairs.map((p) => Math.abs(p.value)), 0);
  const axisLabel = chart?.x_axis_label;

  return (
    <DisplayCardShell label={label} meta={axisLabel}>
      {/* Decorative bar rendering (aria-hidden) — the accessible <table> below is the
          authoritative data path (D-02 accessible fallback). */}
      <div aria-hidden="true" className="flex flex-col gap-2">
        {pairs.map((p, i) => {
          const pct = max > 0 ? Math.round((Math.abs(p.value) / max) * 100) : 0;
          return (
            <div key={i} className="flex items-center gap-2">
              <span
                className="w-24 shrink-0 truncate text-[0.75rem] text-text-muted"
                title={p.label}
              >
                {p.label}
              </span>
              <span className="relative h-4 flex-1 overflow-hidden rounded-[var(--radius-sm)] bg-surface">
                <span
                  className="absolute inset-y-0 left-0 rounded-[var(--radius-sm)] bg-info"
                  style={{ width: `${String(pct)}%` }}
                />
              </span>
              <span className="w-16 shrink-0 text-right font-mono text-[0.75rem] tabular-nums text-text">
                {p.value}
              </span>
            </div>
          );
        })}
      </div>

      {/* Accessible fallback (D-02): a real <table> the bars visualize. */}
      <table className="mt-4 min-w-full border-collapse text-left text-sm">
        <caption className="sr-only">{axisLabel ?? label}</caption>
        <thead>
          <tr>
            <th className="border-b border-border px-3 py-2 text-[0.75rem] font-medium uppercase text-text-faint">
              {axisLabel ?? t('display.chart.category')}
            </th>
            <th className="border-b border-border px-3 py-2 text-right text-[0.75rem] font-medium uppercase text-text-faint">
              {t('display.chart.value')}
            </th>
          </tr>
        </thead>
        <tbody>
          {pairs.map((p, i) => (
            <tr key={i}>
              <td className="border-b border-border px-3 py-2 text-text-muted">{p.label}</td>
              <td className="border-b border-border px-3 py-2 text-right font-mono tabular-nums text-text-muted">
                {p.value}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </DisplayCardShell>
  );
}

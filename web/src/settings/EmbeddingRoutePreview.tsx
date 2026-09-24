import { useState, type ReactNode } from 'react';
import { Check } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { refusalText, type EmbeddingRoutePreview } from './embeddingSpaceApi';
import { Button } from '@/components/ui/button';

interface EmbeddingRoutePreviewCardProps {
  readonly preview: EmbeddingRoutePreview;
  readonly applying: boolean;
  readonly onApply: () => void;
}

function Fact({ label, children }: { readonly label: string; readonly children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 sm:flex-row sm:gap-3">
      <dt className="shrink-0 text-text-faint sm:w-40">{label}</dt>
      <dd className="min-w-0 break-words text-text">{children}</dd>
    </div>
  );
}

// A cost can be a fraction of a cent: two significant digits say it, two decimals would
// print $0.00.
function formatUSD(usd: number, language: string): string {
  return usd < 0.01
    ? usd.toLocaleString(language, { maximumSignificantDigits: 2 })
    : usd.toLocaleString(language, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

// EmbeddingRoutePreviewCard is what a route change costs, measured before anything is written:
// the space it would stamp, the width and speed of the model, the corpus to re-embed and its
// price. Applying takes an explicit confirmation, and a refused route cannot be applied at all.
export function EmbeddingRoutePreviewCard({
  preview,
  applying,
  onApply,
}: EmbeddingRoutePreviewCardProps) {
  const { t, i18n } = useTranslation();
  const [confirmed, setConfirmed] = useState(false);
  const number = (value: number) => Math.round(value).toLocaleString(i18n.language);
  const types = preview.work.types ?? [];
  const rows = types.reduce((sum, typed) => sum + typed.rows, 0);
  const chars = types.reduce((sum, typed) => sum + typed.chars, 0);
  const refused = preview.refusals.length > 0;
  const cost = preview.local
    ? t('embeddingRoute.costLocal')
    : preview.cost_usd === null
      ? t('embeddingRoute.costUnknown')
      : t('embeddingRoute.costValue', { usd: formatUSD(preview.cost_usd, i18n.language) });

  return (
    <section
      aria-labelledby="embedding-route-preview-heading"
      className="flex flex-col gap-3 rounded-[var(--radius-md)] border border-border bg-surface p-4"
    >
      <h4 id="embedding-route-preview-heading" className="text-[14px] font-semibold text-text">
        {t('embeddingRoute.heading')}
      </h4>
      {refused ? (
        <ul role="alert" className="flex flex-col gap-1 text-[13px] text-danger">
          {preview.refusals.map((refusal) => (
            <li key={refusal.code}>{refusalText(t, refusal)}</li>
          ))}
        </ul>
      ) : null}
      {preview.space === '' ? null : (
        <dl className="flex flex-col gap-2 text-[13px]">
          <Fact label={t('embeddingRoute.target')}>
            <span className="font-mono text-[12px]">{preview.space}</span> · {preview.space_label}
          </Fact>
          <Fact label={t('embeddingRoute.width')}>
            {t('embeddingRoute.widthValue', {
              native: number(preview.native_width),
              stored: number(preview.dimensions),
            })}
            {preview.width_warning ? (
              <span className="block text-warning">{t('embeddingRoute.widthWarning')}</span>
            ) : null}
          </Fact>
          <Fact label={t('embeddingRoute.speed')}>
            {t('embeddingRoute.speedValue', { chars: number(preview.chars_per_second) })}
          </Fact>
          <Fact label={t('embeddingRoute.work')}>
            {t('embeddingRoute.workValue', {
              rows: number(rows),
              chars: number(chars),
              tokens: number(preview.tokens),
            })}
          </Fact>
          <Fact label={t('embeddingRoute.duration')}>
            {t('embeddingRoute.durationValue', {
              minutes: number(Math.max(1, Math.ceil(preview.duration_seconds / 60))),
            })}
          </Fact>
          <Fact label={t('embeddingRoute.cost')}>{cost}</Fact>
          <Fact label={t('embeddingRoute.limit')}>
            {t('embeddingRoute.limitValue', { tokens: number(preview.input_limit) })}
            {preview.work.passages_over_limit > 0 ? (
              <span className="block text-warning">
                {t('embeddingRoute.cut', { count: preview.work.passages_over_limit })}
              </span>
            ) : null}
          </Fact>
        </dl>
      )}
      <p className="text-[13px] leading-relaxed text-text-muted">{t('embeddingRoute.lexical')}</p>
      {preview.floors_calibrated ? null : (
        <p className="text-[13px] leading-relaxed text-warning">
          {t('embeddingRoute.uncalibrated')}
        </p>
      )}
      <label className="flex items-start gap-2 text-[13px] text-text">
        <input
          type="checkbox"
          checked={confirmed}
          disabled={refused || applying}
          onChange={(event) => {
            setConfirmed(event.target.checked);
          }}
          className="mt-0.5 size-4 accent-accent"
        />
        {t('embeddingRoute.confirm')}
      </label>
      <div>
        <Button
          type="button"
          disabled={refused || !confirmed || applying}
          aria-busy={applying}
          onClick={onApply}
        >
          {applying ? <Spinner /> : <Check aria-hidden="true" />}
          {t(applying ? 'embeddingRoute.applying' : 'embeddingRoute.apply')}
        </Button>
      </div>
    </section>
  );
}

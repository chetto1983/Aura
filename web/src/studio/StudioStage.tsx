import { Suspense } from 'react';
import { Download, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { GenerationFrame } from '../chat/generation/GenerationFrame';
import { PreviewLoading } from '../chat/artifacts/renderers/PreviewStatus';
import { PreviewByKind } from '../chat/artifacts/renderers/previewDispatch';
import { assetDownloadUrl, type StudioRecord } from './studioApi';
import { studioErrorSentence } from './studioErrors';
import { isActive } from './studioForm';
import { formatEstimate } from './studioPrice';
import { Button } from '@/components/ui/button';

// StudioStage — the centre of the page: the headline before anything exists, then whichever
// generation is selected. A finished one is the artefact itself, with what it cost and the two
// things that can be done with it; a running one is the frame with the job's own clock; a
// failed one is the reason, not a blank rectangle.

/** Whether Reuse can run, and why not when it cannot. Reuse has to turn the record's asset
 *  ids back into images the bar can show, which it cannot do before the library answers and
 *  cannot do at all if that read failed — and a Reuse that quietly drops the reference images
 *  hands back a different, cheaper request than the one that was clicked. */
export type ReuseState = 'ready' | 'waiting' | 'unavailable';

interface StudioStageProps {
  readonly record: StudioRecord | undefined;
  readonly onReuse: (record: StudioRecord) => void;
  readonly reuseState: ReuseState;
}

/** The ratio the frame is drawn at, in CSS form. A record that never carried one (an image
 *  model declaring no ratios) falls back to a square rather than collapsing to zero height. */
function cssRatio(ratio: string | undefined): string {
  return ratio === undefined || ratio === '' ? '1 / 1' : ratio.replace(':', ' / ');
}

export function StudioStage({ record, onReuse, reuseState }: StudioStageProps) {
  const { t } = useTranslation();

  if (record === undefined) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-4 text-center">
        <h1 className="studio-title font-display text-4xl leading-tight font-semibold sm:text-5xl">
          {t('studio.stage.headline')}
        </h1>
        <p className="max-w-md text-sm text-text-muted">{t('studio.stage.idle')}</p>
      </div>
    );
  }

  if (isActive(record)) {
    return (
      <div className="flex w-full max-w-2xl flex-col items-center gap-2">
        <GenerationFrame
          kind={record.kind}
          prompt={record.prompt}
          aspectRatio={cssRatio(record.used.aspect_ratio)}
          generating
          // The job's own creation time, so a reload does not restart the clock on a clip
          // that has already been running for two minutes.
          startedAt={Date.parse(record.created_at)}
        />
      </div>
    );
  }

  if (record.status !== 'completed' || record.asset_id === undefined) {
    return (
      <div
        role="alert"
        className="flex w-full max-w-md flex-col gap-2 rounded-[var(--radius-md)] border border-danger/40 bg-surface p-4 text-center"
      >
        <p className="text-sm font-semibold text-danger">{t('studio.stage.failed')}</p>
        <p className="text-sm text-text-muted">
          {studioErrorSentence(t, record.error?.code ?? '', record.error?.message ?? '')}
        </p>
        {/* `outcome_unknown` means the provider accepted the job and never reported how it
            ended, so it may already be on the bill. Offering Reuse under that sentence is
            offering to pay for the same clip twice. */}
        <StageActions
          record={record}
          onReuse={onReuse}
          reuseState={record.error?.code === 'outcome_unknown' ? 'forbidden' : reuseState}
        />
      </div>
    );
  }

  return (
    <figure className="flex w-full max-w-3xl flex-col items-center gap-3">
      <div className="flex max-h-[58vh] w-full justify-center overflow-hidden">
        <Suspense fallback={<PreviewLoading />}>
          <PreviewByKind
            kind={record.kind}
            asset={{
              assetId: record.asset_id,
              // The record names no MIME type; the asset route labels its own bytes, and
              // relabelling them from a guess here is how a valid image stops rendering.
              mimeType: '',
              fileName: record.prompt,
            }}
            downloadFallback={null}
          />
        </Suspense>
      </div>
      <figcaption className="flex w-full flex-col items-center gap-2">
        <p className="max-w-2xl text-center text-sm text-text-muted">{record.prompt}</p>
        <StageActions record={record} onReuse={onReuse} reuseState={reuseState} />
      </figcaption>
    </figure>
  );
}

function StageActions({
  record,
  onReuse,
  reuseState,
}: {
  readonly record: StudioRecord;
  readonly onReuse: (record: StudioRecord) => void;
  /** 'forbidden' for a record that must not seed another paid attempt at all. */
  readonly reuseState: ReuseState | 'forbidden';
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center justify-center gap-2 text-[11px] text-text-faint">
      <span className="font-mono">{record.model}</span>
      <span className="tabular-nums">
        {record.cost_usd === undefined
          ? t('studio.history.costUnknown')
          : t('studio.history.cost', { cost: formatEstimate(record.cost_usd) })}
      </span>
      {record.adjustments !== undefined && record.adjustments.length > 0 ? (
        <span className="text-warning">
          {t('studio.history.adjusted', { notes: record.adjustments.join(', ') })}
        </span>
      ) : null}
      {record.asset_id === undefined ? null : (
        <Button asChild size="sm" variant="ghost" className="min-h-8 gap-1.5 py-1 text-xs">
          <a href={assetDownloadUrl(record.asset_id)} download>
            <Download aria-hidden="true" className="size-3.5" />
            {t('studio.history.download')}
          </a>
        </Button>
      )}
      {reuseState === 'forbidden' ? null : (
        <Button
          size="sm"
          variant="ghost"
          disabled={reuseState !== 'ready'}
          // A disabled control that does not say why is a bug report waiting to happen; this
          // one says whether the library is still being read or could not be read at all.
          title={
            reuseState === 'waiting'
              ? t('studio.frames.libraryLoading')
              : reuseState === 'unavailable'
                ? t('studio.history.reuseUnavailable')
                : undefined
          }
          className="min-h-8 gap-1.5 py-1 text-xs"
          onClick={() => {
            onReuse(record);
          }}
        >
          <RotateCcw aria-hidden="true" className="size-3.5" />
          {t('studio.history.reuse')}
        </Button>
      )}
    </div>
  );
}

import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { downloadBlob } from '../mediaEdit/download';
import type { VideoProject } from './project';
import { exportProject, type MediaUrls } from './videoflow';
import { exportProjectAudio, NoProjectAudioError } from './videoflow_exportAudio';
import { Button } from '@/components/ui/button';

// VideoStudio_export.tsx — the export, its bar and its cancel, kept whole in one file because
// they are one behaviour: a render that cannot be stopped is not a render with a missing button,
// it is a different feature.
//
// The previous cycle shipped an editor that kept transcoding after its dialog closed. Here the
// signal reaches both renderers, and the unmount aborts too — so leaving the editor stops the
// work rather than orphaning it behind a closed surface.

interface ExportPanelProps {
  readonly project: VideoProject;
  /** What the download is called. The workspace resolves it, because an unnamed project reads
   *  as `videoStudio.untitled` on screen and must read the same on disk. */
  readonly fileName: string;
  readonly audioFileName: string;
  readonly urls: MediaUrls;
  /** Why this project cannot be rendered, already worded — an empty lane, a source the library
   *  no longer holds. The workspace decides, because it is the one that knows; here it is the
   *  difference between a disabled button and a disabled button that says why. */
  readonly refusal?: string | undefined;
}

function reason(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function ExportPanel({ project, fileName, audioFileName, urls, refusal }: ExportPanelProps) {
  const { t } = useTranslation();
  const [percent, setPercent] = useState<number>();
  const [format, setFormat] = useState<'video' | 'audio'>();
  const [failure, setFailure] = useState<string>();
  const running = useRef<AbortController | undefined>(undefined);

  useEffect(
    () => () => {
      running.current?.abort();
    },
    [],
  );

  async function run(selected: 'video' | 'audio') {
    const controller = new AbortController();
    running.current = controller;
    setFailure(undefined);
    setFormat(selected);
    if (selected === 'video') setPercent(0);
    try {
      if (selected === 'video') {
        const blob = await exportProject(project, urls, {
          onProgress: (fraction) => {
            setPercent(Math.round(fraction * 100));
          },
          signal: controller.signal,
        });
        downloadBlob(blob, fileName);
      } else {
        const blob = await exportProjectAudio(project, urls, controller.signal);
        downloadBlob(blob, audioFileName);
      }
    } catch (error) {
      // An abort is the operator's own decision, not a failure to report back to them.
      if (!controller.signal.aborted) {
        setFailure(
          error instanceof NoProjectAudioError
            ? t('videoStudio.export.noAudio')
            : t('videoStudio.export.failed', { reason: reason(error) }),
        );
      }
    } finally {
      running.current = undefined;
      setPercent(undefined);
      setFormat(undefined);
    }
  }

  return (
    <div className="video-studio-export flex flex-wrap items-center gap-2">
      {format === undefined ? null : (
        <>
          {format === 'video' && percent !== undefined ? (
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={percent}
              aria-label={t('videoStudio.export.progress')}
              className="h-1.5 w-24 overflow-hidden rounded-full bg-surface-2"
            >
              <div
                data-progress-fill
                className="h-full rounded-full bg-accent transition-[width] motion-reduce:transition-none"
                style={{ width: `${String(percent)}%` }}
              />
            </div>
          ) : null}
          <span role="status" className="text-xs text-text-muted tabular-nums">
            {format === 'video'
              ? t('videoStudio.export.percent', { percent })
              : t('videoStudio.export.audioWorking')}
          </span>
          <Button type="button" variant="ghost" size="sm" onClick={() => running.current?.abort()}>
            {t('videoStudio.export.cancel')}
          </Button>
        </>
      )}
      <Button
        type="button"
        size="sm"
        disabled={format !== undefined || refusal !== undefined}
        title={refusal}
        onClick={() => void run('video')}
      >
        {t('videoStudio.export.action')}
      </Button>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={format !== undefined || refusal !== undefined}
        title={refusal}
        onClick={() => void run('audio')}
      >
        {t('videoStudio.export.audioAction')}
      </Button>
      {failure === undefined ? null : (
        <p role="alert" className="text-xs text-danger">
          {failure}
        </p>
      )}
    </div>
  );
}

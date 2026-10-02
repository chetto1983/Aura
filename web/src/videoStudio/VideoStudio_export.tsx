import type { TFunction } from 'i18next';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getAsset } from '../chat/attachments/api';
import { downloadBlob } from '../mediaEdit/download';
import type { VideoProject } from './project';
import { exportProject, type MediaUrls } from './videoflow';
import { exportProjectAudio, NoProjectAudioError } from './videoflow_exportAudio';
import { ExportSourceError } from './videoflow_media';
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

/**
 * What a source is called where the operator sees it: its asset row's file name, the name the
 * library lists. The project file holds only the asset id, so the row is read here, once, after
 * the failure — a source added since the project loaded, or a clip's cleaned copy, has a row the
 * load never read. A row that does not answer, or answers with no name, leaves the id: a source
 * whose bytes are gone may have lost its row too.
 */
async function sourceName(assetId: string): Promise<string> {
  try {
    const name = (await getAsset(assetId)).file_name;
    return name === '' ? assetId : name;
  } catch {
    return assetId;
  }
}

/** What the operator reads when an export fails: the source that stopped it, by name, or the
 *  sound export's empty mix, or the error's own reason. */
async function failureText(t: TFunction, error: unknown): Promise<string> {
  if (error instanceof ExportSourceError) {
    const source = await sourceName(error.assetId);
    return error.failure === 'unreachable'
      ? t('videoStudio.export.sourceUnreachable', { source })
      : t('videoStudio.export.sourceUndecodable', { source });
  }
  if (error instanceof NoProjectAudioError) return t('videoStudio.export.noAudio');
  return t('videoStudio.export.failed', {
    reason: error instanceof Error ? error.message : String(error),
  });
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
      // An abort is the operator's own decision, not a failure to report back to them — and it
      // can land while the failing source's name is read, so it is asked again after.
      const text = controller.signal.aborted ? undefined : await failureText(t, error);
      if (text !== undefined && !controller.signal.aborted) setFailure(text);
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
      {format === undefined ? (
        <>
          <Button
            type="button"
            size="sm"
            disabled={refusal !== undefined}
            title={refusal}
            onClick={() => void run('video')}
          >
            {t('videoStudio.export.action')}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="video-studio-export-audio"
            disabled={refusal !== undefined}
            title={refusal}
            onClick={() => void run('audio')}
          >
            {t('videoStudio.export.audioAction')}
          </Button>
        </>
      ) : null}
      {failure === undefined ? null : (
        <p role="alert" className="text-xs text-danger">
          {failure}
        </p>
      )}
    </div>
  );
}

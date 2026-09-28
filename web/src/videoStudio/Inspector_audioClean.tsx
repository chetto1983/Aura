import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { deleteAsset } from '../chat/attachments/api';
import { setClipPresentation } from './commands';
import { recordAnalysis, setAudioProperties } from './commands_audio';
import { sourceOf, type VideoProject } from './project';
import { uploadSource } from './VideoStudio_sources';
import { Switch } from '@/components/ui/switch';

// Inspector_audioClean.tsx — the audio controls that need an analysis in the browser before they
// can edit: noise reduction, and ducking. Each runs its analysis under an AbortSignal, says on the
// control that it is working, and enters the project in ONE edit — the analysis recorded and the
// setting changed together, so one undo takes back both. A failure is said on the control and
// leaves the project untouched; a control that goes away mid-way writes nothing (spec §Browser-side
// analysis). The WASM behind each analysis is imported when it is first asked for.

type Commit = (edit: (current: VideoProject) => VideoProject) => void;

type AnalysisState =
  | { readonly state: 'idle' }
  | { readonly state: 'working' }
  | { readonly state: 'failed'; readonly reason: string };

/** One analysis at a time for a control, aborted when the control goes: mounted with the item's id
 *  as its key, a control that goes is also a control whose selection changed. */
function useAnalysis() {
  const [state, setState] = useState<AnalysisState>({ state: 'idle' });
  const running = useRef<AbortController>(undefined);
  useEffect(
    () => () => {
      running.current?.abort();
    },
    [],
  );
  async function run(task: (signal: AbortSignal) => Promise<void>) {
    running.current?.abort();
    const controller = new AbortController();
    running.current = controller;
    setState({ state: 'working' });
    try {
      await task(controller.signal);
      if (!controller.signal.aborted) setState({ state: 'idle' });
    } catch (error) {
      if (controller.signal.aborted) return;
      setState({ state: 'failed', reason: error instanceof Error ? error.message : String(error) });
    }
  }
  return { state, run };
}

/** What a switch turns: a clip's noise reduction or a sound's, over the source it plays. */
interface CleanTarget {
  readonly kind: 'clip' | 'sound';
  readonly id: string;
  readonly sourceId: string;
  readonly on: boolean;
}

interface NoiseReductionSwitchProps {
  readonly project: VideoProject;
  readonly target: CleanTarget;
  readonly onCommand: Commit;
}

/** Noise reduction for one clip or sound. The first time it is turned on for a source, the source
 *  is cleaned and the copy stored (`denoisedAssetId`); every later time reuses that copy. */
export function NoiseReductionSwitch({ project, target, onCommand }: NoiseReductionSwitchProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const id = useId();
  const { state, run } = useAnalysis();
  const source = sourceOf(project, target.sourceId);
  if (source === undefined || source.kind === 'image' || source.hasAudio === false) return null;
  const known = source;

  const turned = (current: VideoProject, denoise: boolean) =>
    target.kind === 'clip'
      ? setClipPresentation(current, { clipId: target.id, denoise })
      : setAudioProperties(current, { itemId: target.id, denoise });

  function turn(on: boolean) {
    if (!on || known.denoisedAssetId !== undefined) {
      onCommand((current) => turned(current, on));
      return;
    }
    void run(async (signal) => {
      const { cleanedFile } = await import('./audioClean');
      const file = await cleanedFile(assetUrl(known.assetId), known.id, signal);
      signal.throwIfAborted();
      const assetId = await uploadSource(file);
      if (signal.aborted) {
        // The upload could not be stopped once started: the copy it made is deleted rather than
        // left in the library with nothing pointing at it. The control is gone, so a failed delete
        // has nobody to tell.
        await deleteAsset(assetId).catch(() => undefined);
        return;
      }
      onCommand((current) =>
        turned(recordAnalysis(current, { sourceId: known.id, denoisedAssetId: assetId }), true),
      );
    });
  }

  return (
    <div className="grid gap-1">
      <div className="flex items-center gap-2 text-xs text-text-muted">
        <Switch
          id={id}
          checked={target.on}
          disabled={state.state === 'working'}
          aria-label={t('videoStudio.audio.denoise')}
          onCheckedChange={turn}
        />
        <label htmlFor={id}>{t('videoStudio.audio.denoise')}</label>
      </div>
      {state.state === 'working' ? (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.audio.cleaning')}
        </p>
      ) : null}
      {state.state === 'failed' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.cleanFailed', { reason: state.reason })}
        </p>
      ) : null}
    </div>
  );
}

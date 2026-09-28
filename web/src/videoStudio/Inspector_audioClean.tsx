import { use, useId } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { deleteAsset } from '../chat/attachments/api';
import { useAnalysis } from './analysisState';
import { AnalysisStatus } from './analysisStatus';
import { setClipPresentation } from './commands';
import { recordAnalysis, setAudioProperties } from './commands_audio';
import { sourceOf, type AudioDucking, type AudioItem, type VideoProject } from './project';
import { unheardSources } from './videoflow_audio';
import { DuckingListeningContext, listeningState } from './VideoStudio_ducking';
import { uploadSource } from './VideoStudio_sources';
import { Button } from '@/components/ui/button';
import { Slider } from '@/components/ui/slider';
import { Switch } from '@/components/ui/switch';

// Inspector_audioClean.tsx — the audio controls that need an analysis in the browser before they
// can edit: noise reduction, and ducking. Each runs its analysis under an AbortSignal, says on the
// control that it is working, and enters the project in ONE edit — the analysis recorded and the
// setting changed together, so one undo takes back both. A failure is said on the control and
// leaves the project untouched; a control that goes away mid-way writes nothing (spec §Browser-side
// analysis). The WASM behind each analysis is imported when it is first asked for. Once ducking is
// on, a sound added under it is listened to by the workspace itself (VideoStudio_ducking.ts); the
// ducking control only says so.

type Commit = (edit: (current: VideoProject) => VideoProject) => void;

/** What a switch turns: a clip's noise reduction or a sound's, over the source it plays. */
interface CleanTarget {
  readonly kind: 'clip' | 'sound';
  readonly id: string;
  readonly sourceId: string;
  /** What the sound is called — its file, its text, its recording — and so its cleaned copy. */
  readonly label?: string | undefined;
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
      const file = await cleanedFile(assetUrl(known.assetId), target.label ?? target.kind, signal);
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
      <AnalysisStatus
        state={state}
        working="videoStudio.audio.cleaning"
        failed="videoStudio.audio.cleanFailed"
      />
    </div>
  );
}

/** Ducking as it is first turned on: the spec's defaults (spec §T7). */
const DEFAULT_DUCKING: AudioDucking = { amountDb: -12, ramp: 0.5 };

interface DuckingControlsProps {
  readonly project: VideoProject;
  readonly item: AudioItem;
  readonly onCommand: Commit;
}

/** Lower this sound under the speech the rest of the film carries. Turned on, the speech of every
 *  source it goes down under is listened for once and recorded on the source, with the switch, in
 *  one edit; a source added later is listened to by the workspace itself, and this control says
 *  so — or why it could not be heard, with a second try. */
export function DuckingControls({ project, item, onCommand }: DuckingControlsProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const id = useId();
  const { state, run } = useAnalysis();
  const workspace = use(DuckingListeningContext);
  const ducking = item.ducking;
  const unheard = unheardSources(project, item.id);
  // Its own listen while it turns on, else the workspace's for the sources it waits on.
  const shown =
    state.state === 'idle' && ducking !== undefined && unheard.length > 0
      ? listeningState(workspace)
      : state;

  const duck = (current: VideoProject, next: AudioDucking | null) =>
    setAudioProperties(current, { itemId: item.id, ducking: next });

  function turn(on: boolean) {
    if (!on) {
      onCommand((current) => duck(current, null));
      return;
    }
    if (unheard.length === 0) {
      onCommand((current) => duck(current, DEFAULT_DUCKING));
      return;
    }
    void run(async (signal) => {
      const { detectSpeech } = await import('./audioSpeech');
      const heard: { sourceId: string; speech: (readonly [number, number])[] }[] = [];
      for (const source of unheard) {
        heard.push({
          sourceId: source.id,
          speech: await detectSpeech(assetUrl(source.assetId), signal),
        });
      }
      signal.throwIfAborted();
      onCommand((current) =>
        duck(
          heard.reduce((next, analysis) => recordAnalysis(next, analysis), current),
          DEFAULT_DUCKING,
        ),
      );
    });
  }

  return (
    <div className="grid gap-2">
      <div className="flex items-center gap-2 text-xs text-text-muted">
        <Switch
          id={id}
          checked={ducking !== undefined}
          disabled={state.state === 'working'}
          aria-label={t('videoStudio.audio.ducking')}
          onCheckedChange={turn}
        />
        <label htmlFor={id}>{t('videoStudio.audio.ducking')}</label>
      </div>
      {ducking === undefined ? null : (
        <>
          <label className="video-studio-adjustment">
            <span>{t('videoStudio.audio.duckAmount')}</span>
            <span className="font-mono tabular-nums">{ducking.amountDb} dB</span>
            <Slider
              key={ducking.amountDb}
              aria-label={t('videoStudio.audio.duckAmount')}
              min={-24}
              max={-3}
              step={1}
              defaultValue={[ducking.amountDb]}
              onValueCommit={([amountDb = ducking.amountDb]) => {
                onCommand((current) => duck(current, { ...ducking, amountDb }));
              }}
            />
          </label>
          <label className="video-studio-adjustment">
            <span>{t('videoStudio.audio.duckSoftness')}</span>
            <span className="font-mono tabular-nums">{ducking.ramp.toFixed(1)}s</span>
            <Slider
              key={ducking.ramp}
              aria-label={t('videoStudio.audio.duckSoftness')}
              min={0.1}
              max={2}
              step={0.1}
              defaultValue={[ducking.ramp]}
              onValueCommit={([ramp = ducking.ramp]) => {
                // A float step lands on 0.6000000000000001: rounded before the range check and
                // the saved file see it.
                onCommand((current) =>
                  duck(current, { ...ducking, ramp: Math.round(ramp * 10) / 10 }),
                );
              }}
            />
          </label>
          {shown.state === 'failed' && state.state === 'idle' ? (
            <Button type="button" variant="outline" size="sm" onClick={workspace.retry}>
              {t('videoStudio.audio.listenAgain')}
            </Button>
          ) : null}
        </>
      )}
      <AnalysisStatus
        state={shown}
        working="videoStudio.audio.listening"
        failed="videoStudio.audio.listenFailed"
      />
    </div>
  );
}

import { createContext, useEffect, useEffectEvent, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { reasonOf, type AnalysisState } from './analysisState';
import { recordAnalysis } from './commands_audio';
import type { Edit } from './history';
import type { VideoProject } from './project';
import type { SpeechWindow } from './speechWindows';
import { unheardByDucking } from './videoflow_audio';

// VideoStudio_ducking.ts — ducking listens by itself (operator, 2026-09-28: "Listen by itself").
// A sound added or unmuted while another sound ducks is listened to without being asked, one source
// at a time, through the detector the Ducking switch uses. What it hears is a measurement of the
// source's bytes, not an edit: it is written beside the history (History.annotate), so an undo
// takes back what the operator did and never a measurement, and a redo is never lost to one. It is
// said wherever the operator is — the workspace's status line — and on the ducking sound's own
// control, and the export waits until everything ducking goes under has been heard.

export interface DuckingListening {
  /** A source some ducking sound goes under is being listened to now. */
  readonly listening: boolean;
  /** Why a source could not be heard. It is not listened to again until `retry`. */
  readonly failure: string | undefined;
  /** Why the export has to wait, worded; nothing once ducking has heard all it goes under. */
  readonly refusal: string | undefined;
  readonly retry: () => void;
}

export const DuckingListeningContext = createContext<DuckingListening>({
  listening: false,
  failure: undefined,
  refusal: undefined,
  retry: () => undefined,
});

/** Ducking's listening as an analysis a control shows: a failure first, since it waits on the
 *  operator, then the work under way. */
export function listeningState({ listening, failure }: DuckingListening): AnalysisState {
  if (failure !== undefined) return { state: 'failed', reason: failure };
  return listening ? { state: 'working' } : { state: 'idle' };
}

export function useDuckingListener(
  project: VideoProject | undefined,
  annotate: (edit: Edit) => void,
): DuckingListening {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const [failed, setFailed] = useState<ReadonlyMap<string, string>>(() => new Map());
  // What each asset was heard to say, for as long as the editor is open: a redo brings a source
  // back the way its step recorded it — unheard — and it is written again without a second listen.
  const heard = useRef(new Map<string, SpeechWindow[]>());
  const needed = project === undefined ? [] : unheardByDucking(project);
  const next = needed.find((source) => !failed.has(source.assetId));
  const sourceId = next?.id;
  const assetId = next?.assetId;

  const hear = useEffectEvent(async (asset: string, signal: AbortSignal) => {
    const known = heard.current.get(asset);
    if (known !== undefined) return known;
    const { detectSpeech } = await import('./audioSpeech');
    const speech = await detectSpeech(assetUrl(asset), signal);
    heard.current.set(asset, speech);
    return speech;
  });
  const record = useEffectEvent((source: string, speech: SpeechWindow[]) => {
    annotate((current) => recordAnalysis(current, { sourceId: source, speech }));
  });

  useEffect(() => {
    if (sourceId === undefined || assetId === undefined) return undefined;
    const controller = new AbortController();
    hear(assetId, controller.signal).then(
      (speech) => {
        if (!controller.signal.aborted) record(sourceId, speech);
      },
      (error: unknown) => {
        if (!controller.signal.aborted) {
          setFailed((current) => new Map(current).set(assetId, reasonOf(error)));
        }
      },
    );
    return () => {
      controller.abort();
    };
  }, [sourceId, assetId]);

  const failure = needed
    .map((source) => failed.get(source.assetId))
    .find((reason) => reason !== undefined);
  const refusal =
    needed.length === 0
      ? undefined
      : failure === undefined
        ? t('videoStudio.audio.exportListening')
        : t('videoStudio.audio.exportUnheard', { reason: failure });
  return {
    listening: next !== undefined,
    failure,
    refusal,
    retry: () => {
      setFailed(new Map());
    },
  };
}

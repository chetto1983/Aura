import { RotateCcw, RotateCw, Play, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Asset } from '../chat/attachments/types';
import { CropOverlay } from './CropOverlay';
import {
  CROP_PRESETS,
  presetRect,
  rotateSize,
  type CropPreset,
  type CropRect,
  type Rotation,
} from './cropMath';
import { downloadBlob } from './download';
import {
  editedName,
  typedRange,
  videoContainer,
  type BlockedTrack,
  type VideoEdit,
} from './editRules';
import { MediaEditorLayer } from './MediaEditorLayer';
import { TimeField } from './TimeField';
import { formatTimecode } from './timecode';
import { useObjectUrl } from './useObjectUrl';
import { exportVideo, filmstrip, probeVideo, type VideoInfo } from './videoMedia';
import { VideoTimeline } from './VideoTimeline';
import { Button } from '@/components/ui/button';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Switch } from '@/components/ui/switch';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// VideoEditor — trim, crop, rotate and mute a clip in the browser, after the layout of Adobe
// Express's and 123apps' trimmers: preview on top, filmstrip below, Start/End and Save last.

export interface EditorProps {
  readonly asset: Asset;
  readonly source: Blob;
  readonly onClose: () => void;
}

type Tool = 'trim' | 'crop' | 'rotate' | 'audio';
const TOOLS: readonly Tool[] = ['trim', 'crop', 'rotate', 'audio'];
const FILMSTRIP_FRAMES = 10;

function turn(rotation: Rotation, quarter: 1 | -1): Rotation {
  return ((((rotation + quarter * 90) % 360) + 360) % 360) as Rotation;
}

export default function VideoEditor({ asset, source, onClose }: EditorProps) {
  const { t } = useTranslation();
  const url = useObjectUrl(source);
  const videoRef = useRef<HTMLVideoElement>(null);
  const abortRef = useRef<AbortController | undefined>(undefined);
  const [info, setInfo] = useState<VideoInfo>();
  const [frames, setFrames] = useState<CanvasImageSource[]>([]);
  const [tool, setTool] = useState<Tool>('trim');
  const [range, setRange] = useState({ start: 0, end: 0 });
  const [rotation, setRotation] = useState<Rotation>(0);
  const [preset, setPreset] = useState<CropPreset>('original');
  const [moved, setMoved] = useState<CropRect>();
  const [mute, setMute] = useState(false);
  const [progress, setProgress] = useState<number>();
  const [problem, setProblem] = useState<string>();
  const [unreadable, setUnreadable] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    probeVideo(source, controller.signal).then(
      (probed) => {
        if (controller.signal.aborted) return;
        setInfo(probed);
        setRange({ start: 0, end: probed.duration });
      },
      () => {
        if (!controller.signal.aborted) setUnreadable(true);
      },
    );
    return () => {
      controller.abort();
    };
  }, [source]);

  useEffect(() => {
    const controller = new AbortController();
    filmstrip(source, FILMSTRIP_FRAMES, 90, controller.signal).then(
      (strip) => {
        if (!controller.signal.aborted) setFrames(strip);
      },
      // The pictures are decoration: without them the timeline still trims.
      () => undefined,
    );
    return () => {
      controller.abort();
    };
  }, [source]);

  // An export outlives nothing: leaving the editor stops it, so no download lands afterwards.
  useEffect(
    () => () => {
      abortRef.current?.abort();
    },
    [],
  );

  const frame = info === undefined ? undefined : rotateSize(info, rotation);
  const crop =
    frame === undefined || preset === 'original' ? undefined : (moved ?? presetRect(frame, preset));
  const output = crop ?? (frame === undefined ? undefined : presetRect(frame, 'original'));

  function blockedSentence(tracks: readonly BlockedTrack[]): string {
    const first = tracks[0];
    if (first === undefined) return t('mediaEdit.video.blockedUnknown');
    const track =
      first.type === 'video' ? t('mediaEdit.video.trackVideo') : t('mediaEdit.video.trackAudio');
    return t('mediaEdit.video.blocked', {
      track,
      codec: first.codec ?? t('mediaEdit.video.codecUnknown'),
    });
  }

  async function save() {
    const controller = new AbortController();
    abortRef.current = controller;
    setProblem(undefined);
    setProgress(0);
    const base = { start: range.start, end: range.end, rotation, mute };
    const edit: VideoEdit = crop === undefined ? base : { ...base, crop };
    try {
      const result = await exportVideo(
        source,
        asset.mime_type,
        edit,
        setProgress,
        controller.signal,
      );
      if (result.kind === 'blocked') setProblem(blockedSentence(result.tracks));
      if (result.kind === 'empty') setProblem(t('mediaEdit.video.emptyExport'));
      if (result.kind === 'done') {
        downloadBlob(
          result.blob,
          editedName(asset.file_name, t('mediaEdit.suffix.video'), videoContainer(asset.mime_type)),
        );
      }
    } catch (error) {
      setProblem(
        t('mediaEdit.video.failed', {
          reason: error instanceof Error ? error.message : String(error),
        }),
      );
    } finally {
      abortRef.current = undefined;
      setProgress(undefined);
    }
  }

  function close() {
    abortRef.current?.abort();
    onClose();
  }

  function reset() {
    if (info !== undefined) setRange({ start: 0, end: info.duration });
    setRotation(0);
    setPreset('original');
    setMoved(undefined);
    setMute(false);
  }

  function playSelection() {
    const video = videoRef.current;
    if (video === null) return;
    video.currentTime = range.start;
    video.play().catch((error: unknown) => {
      // A pause that interrupts play() rejects with AbortError: nothing went wrong.
      if (error instanceof DOMException && error.name === 'AbortError') return;
      setProblem(t('mediaEdit.video.playFailed'));
    });
  }

  const quarter = rotation === 90 || rotation === 270;
  const percent = progress === undefined ? undefined : Math.round(progress * 100);
  const toolLabel = (item: Tool) => t(`mediaEdit.video.tool.${item}`);

  return (
    <MediaEditorLayer label={t('mediaEdit.editName', { name: asset.file_name })} onEscape={close}>
      <header className="flex items-center gap-3 border-b border-border px-4 py-2">
        <h2 className="min-w-0 flex-1 truncate font-mono text-sm">{asset.file_name}</h2>
        <Button type="button" variant="ghost" size="sm" onClick={reset}>
          {t('mediaEdit.video.reset')}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t('mediaEdit.close')}
          onClick={close}
        >
          <X aria-hidden="true" className="size-4" />
        </Button>
      </header>

      <div className="flex items-center gap-2 px-4 py-2">
        <ToggleGroup
          type="single"
          value={tool}
          onValueChange={(value) => {
            if (value !== '') setTool(value as Tool);
          }}
          aria-label={t('mediaEdit.video.tools')}
          className="hidden sm:flex"
        >
          {TOOLS.map((item) => (
            <ToggleGroupItem key={item} value={item}>
              {toolLabel(item)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <NativeSelect
          aria-label={t('mediaEdit.video.tools')}
          value={tool}
          onChange={(event) => {
            setTool(event.target.value as Tool);
          }}
          className="sm:hidden"
        >
          {TOOLS.map((item) => (
            <NativeSelectOption key={item} value={item}>
              {toolLabel(item)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>

      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 overflow-auto px-4">
        {unreadable ? (
          <p role="alert" className="text-sm text-danger">
            {t('mediaEdit.loadFailed')}
          </p>
        ) : frame === undefined || url === undefined ? (
          <p role="status" className="text-sm text-text-muted">
            {t('mediaEdit.loading')}
          </p>
        ) : (
          <div
            className="relative w-full overflow-hidden rounded-[var(--radius-md)] bg-black"
            style={{
              aspectRatio: `${String(frame.width)} / ${String(frame.height)}`,
              maxWidth: `min(100%, calc(52vh * ${String(frame.width / frame.height)}))`,
            }}
          >
            {/* eslint-disable-next-line jsx-a11y/media-has-caption -- the operator's own clip carries no captions and this element is a preview, not playback of content. */}
            <video
              ref={videoRef}
              src={url}
              muted={mute}
              playsInline
              onTimeUpdate={(event) => {
                if (event.currentTarget.currentTime >= range.end) event.currentTarget.pause();
              }}
              className="absolute top-1/2 left-1/2 max-w-none object-fill"
              style={{
                width: quarter ? `${String((frame.height / frame.width) * 100)}%` : '100%',
                height: quarter ? `${String((frame.width / frame.height) * 100)}%` : '100%',
                transform: `translate(-50%, -50%) rotate(${String(rotation)}deg)`,
              }}
            />
            {crop === undefined ? null : (
              <CropOverlay
                frame={frame}
                rect={crop}
                onMove={setMoved}
                label={t('mediaEdit.video.cropArea')}
              />
            )}
          </div>
        )}

        {tool === 'crop' && frame !== undefined ? (
          <div
            role="group"
            aria-label={t('mediaEdit.video.presets')}
            className="flex flex-wrap justify-center gap-1"
          >
            {CROP_PRESETS.map((item) => (
              <Button
                key={item}
                type="button"
                size="sm"
                variant={preset === item ? 'default' : 'ghost'}
                aria-pressed={preset === item}
                onClick={() => {
                  setPreset(item);
                  setMoved(undefined);
                }}
              >
                {item === 'original' ? t('mediaEdit.video.original') : item}
              </Button>
            ))}
          </div>
        ) : null}

        {tool === 'rotate' ? (
          <div className="flex gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t('mediaEdit.video.rotateLeft')}
              onClick={() => {
                setRotation(turn(rotation, -1));
                setMoved(undefined);
              }}
            >
              <RotateCcw aria-hidden="true" className="size-4" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t('mediaEdit.video.rotateRight')}
              onClick={() => {
                setRotation(turn(rotation, 1));
                setMoved(undefined);
              }}
            >
              <RotateCw aria-hidden="true" className="size-4" />
            </Button>
          </div>
        ) : null}

        {tool === 'audio' && info !== undefined ? (
          <div className="flex items-center gap-2 text-sm">
            <Switch
              id="media-edit-mute"
              checked={mute}
              disabled={!info.hasAudio}
              onCheckedChange={setMute}
              aria-label={t('mediaEdit.video.mute')}
            />
            <label htmlFor="media-edit-mute">{t('mediaEdit.video.mute')}</label>
            {info.hasAudio ? null : (
              <span className="text-text-muted">{t('mediaEdit.video.noAudio')}</span>
            )}
          </div>
        ) : null}
      </div>

      {info === undefined ? null : (
        <footer className="flex flex-col gap-3 border-t border-border px-4 py-3">
          <VideoTimeline
            duration={info.duration}
            start={range.start}
            end={range.end}
            frames={frames}
            onChange={(start, end) => {
              setRange({ start, end });
            }}
            startLabel={t('mediaEdit.video.startHandle')}
            endLabel={t('mediaEdit.video.endHandle')}
          />
          <div className="flex flex-wrap items-end gap-3">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t('mediaEdit.video.play')}
              onClick={playSelection}
            >
              <Play aria-hidden="true" className="size-4" />
            </Button>
            <TimeField
              key={`start-${formatTimecode(range.start)}`}
              label={t('mediaEdit.video.start')}
              value={range.start}
              onCommit={(value) => {
                setRange(typedRange(range, 'start', value, info.duration));
              }}
            />
            <TimeField
              key={`end-${formatTimecode(range.end)}`}
              label={t('mediaEdit.video.end')}
              value={range.end}
              onCommit={(value) => {
                setRange(typedRange(range, 'end', value, info.duration));
              }}
            />
            {output === undefined ? null : (
              <span className="font-mono text-xs text-text-muted tabular-nums">
                {t('mediaEdit.video.size', { width: output.width, height: output.height })}
              </span>
            )}
            <div className="ms-auto flex items-center gap-2">
              {percent === undefined ? null : (
                <>
                  {/* The components/ui set has no progress bar: this is ContextBudgetGauge's. */}
                  <div
                    role="progressbar"
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={percent}
                    aria-label={t('mediaEdit.video.progress')}
                    className="h-1.5 w-24 overflow-hidden rounded-full bg-surface-2"
                  >
                    <div
                      data-progress-fill
                      className="h-full rounded-full bg-accent transition-[width] motion-reduce:transition-none"
                      style={{ width: `${String(percent)}%` }}
                    />
                  </div>
                  <span role="status" className="text-xs text-text-muted tabular-nums">
                    {t('mediaEdit.video.saving', { percent })}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => abortRef.current?.abort()}
                  >
                    {t('mediaEdit.video.cancel')}
                  </Button>
                </>
              )}
              <Button
                type="button"
                size="sm"
                disabled={progress !== undefined}
                onClick={() => void save()}
              >
                {t('mediaEdit.video.save')}
              </Button>
            </div>
          </div>
          {problem === undefined ? null : (
            <p role="alert" className="text-sm text-danger">
              {problem}
            </p>
          )}
        </footer>
      )}
    </MediaEditorLayer>
  );
}

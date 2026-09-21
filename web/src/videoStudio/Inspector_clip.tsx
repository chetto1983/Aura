import {
  ArrowDown,
  ArrowUp,
  Ban,
  CircleDashed,
  FlipHorizontal2,
  FlipVertical2,
  ScanLine,
  Sparkles,
  RotateCcw,
  RotateCw,
  Zap,
  ZoomIn,
  type LucideIcon,
} from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { CROP_PRESETS, presetRect, rotateSize } from '../mediaEdit/cropMath';
import { formatTimecode } from '../mediaEdit/timecode';
import { TimeField } from '../mediaEdit/TimeField';
import { removeRange, setClipPresentation, setFrameSize, setMuted, trimClip } from './commands';
import {
  clipStart,
  sourceOf,
  type ClipTransition,
  type VideoItem,
  type VideoProject,
} from './project';
import { Button } from '@/components/ui/button';
import { Slider } from '@/components/ui/slider';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

type Commit = (edit: (current: VideoProject) => VideoProject) => void;
type TransformMode = 'fill' | 'fit' | 'crop';
export type ClipTab = 'transform' | 'animation' | 'adjust' | 'audio' | 'speed' | 'time';

interface ClipInspectorProps {
  readonly project: VideoProject;
  readonly clip: VideoItem;
  readonly onCommand: Commit;
  readonly activeTab?: ClipTab;
  readonly onTabChange?: (tab: ClipTab) => void;
}

function FrameControls({ project, clip, onCommand }: ClipInspectorProps) {
  const { t } = useTranslation();
  const [mode, setMode] = useState<TransformMode>(clip.fit === 'contain' ? 'fit' : 'fill');
  const rotation = clip.rotation ?? 0;
  const source = sourceOf(project, clip.sourceId);
  const frame = rotateSize(source?.size ?? project.size, rotation);
  const turn = (quarter: 1 | -1) =>
    ((((rotation + quarter * 90) % 360) + 360) % 360) as 0 | 90 | 180 | 270;
  function present(values: Parameters<typeof setClipPresentation>[1]) {
    onCommand((current) => setClipPresentation(current, values));
  }
  return (
    <div className="video-studio-tab-panel">
      <ToggleGroup
        type="single"
        value={mode}
        variant="outline"
        className="grid w-full grid-cols-3"
        onValueChange={(value) => {
          if (value === '') return;
          const next = value as TransformMode;
          setMode(next);
          if (next !== 'crop') {
            present({ clipId: clip.id, fit: next === 'fit' ? 'contain' : 'cover' });
          }
        }}
      >
        {(['fill', 'fit', 'crop'] as const).map((value) => (
          <ToggleGroupItem key={value} value={value} className="w-full">
            {t(`videoStudio.inspector.transform.${value}`)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      {mode === 'crop' ? (
        <div className="flex flex-wrap gap-1">
          {CROP_PRESETS.map((preset) => (
            <Button
              key={preset}
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                const crop = presetRect(frame, preset);
                onCommand((current) =>
                  setFrameSize(
                    setClipPresentation(current, { clipId: clip.id, fit: 'cover' }),
                    crop,
                  ),
                );
              }}
            >
              {preset === 'original' ? t('mediaEdit.video.original') : preset}
            </Button>
          ))}
        </div>
      ) : null}
      <div className="video-studio-transform-heading">
        <h4 className="video-studio-field-heading">
          {t('videoStudio.inspector.transform.flipRotate')}
        </h4>
        <output className="video-studio-angle">{rotation}°</output>
      </div>
      <div className="video-studio-transform-actions">
        <Button
          type="button"
          variant={clip.flipX === true ? 'default' : 'ghost'}
          className="video-studio-transform-action"
          aria-label={t('videoStudio.inspector.transform.flipHorizontal')}
          onClick={() => {
            present({ clipId: clip.id, flipX: clip.flipX !== true });
          }}
        >
          <FlipHorizontal2 aria-hidden="true" />
          <span>{t('videoStudio.inspector.transform.flipHorizontal')}</span>
        </Button>
        <Button
          type="button"
          variant={clip.flipY === true ? 'default' : 'ghost'}
          className="video-studio-transform-action"
          aria-label={t('videoStudio.inspector.transform.flipVertical')}
          onClick={() => {
            present({ clipId: clip.id, flipY: clip.flipY !== true });
          }}
        >
          <FlipVertical2 aria-hidden="true" />
          <span>{t('videoStudio.inspector.transform.flipVertical')}</span>
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="video-studio-transform-action"
          aria-label={t('mediaEdit.video.rotateLeft')}
          onClick={() => {
            present({ clipId: clip.id, rotation: turn(-1) });
          }}
        >
          <RotateCcw aria-hidden="true" />
          <span>{t('mediaEdit.video.rotateLeft')}</span>
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="video-studio-transform-action"
          aria-label={t('mediaEdit.video.rotateRight')}
          onClick={() => {
            present({ clipId: clip.id, rotation: turn(1) });
          }}
        >
          <RotateCw aria-hidden="true" />
          <span>{t('mediaEdit.video.rotateRight')}</span>
        </Button>
      </div>
    </div>
  );
}

type AdjustmentKey = 'opacity' | 'brightness' | 'contrast' | 'saturation' | 'hue' | 'blur';

function AdjustControl({
  label,
  value,
  min,
  max,
  onCommit,
}: {
  readonly label: string;
  readonly value: number;
  readonly min: number;
  readonly max: number;
  readonly onCommit: (value: number) => void;
}) {
  return (
    <label className="video-studio-adjustment">
      <span>{label}</span>
      <span className="font-mono tabular-nums">{Math.round(value)}</span>
      <Slider
        key={value}
        aria-label={label}
        min={min}
        max={max}
        step={1}
        defaultValue={[value]}
        onValueCommit={([next = value]) => {
          onCommit(next);
        }}
      />
    </label>
  );
}

function AdjustControls({ clip, onCommand }: ClipInspectorProps) {
  const { t } = useTranslation();
  const values: Record<AdjustmentKey, number> = {
    opacity: (clip.opacity ?? 1) * 100,
    brightness: ((clip.brightness ?? 1) - 1) * 100,
    contrast: ((clip.contrast ?? 1) - 1) * 100,
    saturation: ((clip.saturation ?? 1) - 1) * 100,
    hue: clip.hue ?? 0,
    blur: (clip.blur ?? 0) * 50,
  };
  const ranges: Record<AdjustmentKey, readonly [number, number]> = {
    opacity: [0, 100],
    brightness: [-100, 100],
    contrast: [-100, 100],
    saturation: [-100, 100],
    hue: [-180, 180],
    blur: [0, 100],
  };
  return (
    <div className="video-studio-tab-panel">
      {(Object.keys(values) as AdjustmentKey[]).map((key) => (
        <AdjustControl
          key={key}
          label={t(`videoStudio.inspector.adjust.${key}`)}
          value={values[key]}
          min={ranges[key][0]}
          max={ranges[key][1]}
          onCommit={(value) => {
            const normalized =
              key === 'hue'
                ? value
                : key === 'blur'
                  ? value / 50
                  : key === 'opacity'
                    ? value / 100
                    : 1 + value / 100;
            onCommand((current) =>
              setClipPresentation(current, { clipId: clip.id, [key]: normalized }),
            );
          }}
        />
      ))}
    </div>
  );
}

function AudioControls({ clip, onCommand }: ClipInspectorProps) {
  const { t } = useTranslation();
  const id = useId();
  const volume = clip.volume ?? 1;
  return (
    <div className="video-studio-tab-panel">
      <label className="video-studio-adjustment">
        <span>{t('videoStudio.inspector.volume')}</span>
        <span className="font-mono tabular-nums">{Math.round(volume * 100)}%</span>
        <Slider
          key={volume}
          aria-label={t('videoStudio.inspector.volume')}
          min={0}
          max={200}
          step={1}
          defaultValue={[Math.round(volume * 100)]}
          onValueCommit={([next = 100]) => {
            onCommand((current) =>
              setClipPresentation(current, { clipId: clip.id, volume: next / 100 }),
            );
          }}
        />
      </label>
      <div className="flex items-center gap-2 text-xs text-text-muted">
        <Switch
          id={id}
          checked={clip.muted}
          aria-label={t('videoStudio.inspector.mute')}
          onCheckedChange={(muted) => {
            onCommand((current) => setMuted(current, { clipId: clip.id, muted }));
          }}
        />
        <label htmlFor={id}>{t('videoStudio.inspector.mute')}</label>
      </div>
    </div>
  );
}

function SpeedControls({ clip, onCommand }: ClipInspectorProps) {
  const { t } = useTranslation();
  const speed = clip.speed ?? 1;
  const commit = (next: number) => {
    onCommand((current) => setClipPresentation(current, { clipId: clip.id, speed: next }));
  };
  return (
    <div className="video-studio-tab-panel">
      <label className="video-studio-adjustment">
        <span>{t('videoStudio.inspector.playbackRate')}</span>
        <span className="font-mono tabular-nums">{speed.toFixed(2)}×</span>
        <Slider
          key={speed}
          aria-label={t('videoStudio.inspector.playbackRate')}
          min={25}
          max={400}
          step={5}
          defaultValue={[Math.round(speed * 100)]}
          onValueCommit={([next = 100]) => {
            commit(next / 100);
          }}
        />
      </label>
      <div className="grid grid-cols-4 gap-1">
        {[0.5, 1, 1.5, 2].map((value) => (
          <Button
            key={value}
            type="button"
            variant={speed === value ? 'default' : 'ghost'}
            size="sm"
            onClick={() => {
              commit(value);
            }}
          >
            {value}×
          </Button>
        ))}
      </div>
    </div>
  );
}

function TimeControls({ project, clip, onCommand }: ClipInspectorProps) {
  const { t } = useTranslation();
  const end = clip.sourceStart + clip.duration;
  const [from, setFrom] = useState(clip.sourceStart);
  const [to, setTo] = useState(end);
  return (
    <div className="video-studio-tab-panel">
      <TimeField
        key={`start-${clip.id}-${formatTimecode(clip.sourceStart)}`}
        label={t('videoStudio.inspector.start')}
        value={clip.sourceStart}
        onCommit={(at) => {
          onCommand((current) =>
            trimClip(current, {
              clipId: clip.id,
              start: at - clip.sourceStart,
              end: clip.duration,
            }),
          );
        }}
      />
      <TimeField
        key={`end-${clip.id}-${formatTimecode(end)}`}
        label={t('videoStudio.inspector.end')}
        value={end}
        onCommit={(at) => {
          onCommand((current) =>
            trimClip(current, { clipId: clip.id, start: 0, end: at - clip.sourceStart }),
          );
        }}
      />
      <TimeField label={t('videoStudio.inspector.rangeFrom')} value={from} onCommit={setFrom} />
      <TimeField label={t('videoStudio.inspector.rangeTo')} value={to} onCommit={setTo} />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => {
          const base = clipStart(project, clip.id) ?? 0;
          const inLane = (source: number) => base + (source - clip.sourceStart);
          onCommand((current) => removeRange(current, { from: inLane(from), to: inLane(to) }));
        }}
      >
        {t('videoStudio.inspector.removeRange')}
      </Button>
    </div>
  );
}

function AnimationControls({ clip, onCommand }: ClipInspectorProps) {
  const { t } = useTranslation();
  const [edge, setEdge] = useState<'in' | 'out'>('in');
  const [detailsOpen, setDetailsOpen] = useState(false);
  const selected: ClipTransition =
    edge === 'in'
      ? (clip.transitionIn ??
        (clip.fadeIn === true || clip.animation === 'fadeIn' ? 'fade' : 'none'))
      : (clip.transitionOut ??
        (clip.fadeOut === true || clip.animation === 'fadeOut' ? 'fade' : 'none'));
  const presets: readonly { readonly id: ClipTransition; readonly icon: LucideIcon }[] = [
    { id: 'none', icon: Ban },
    { id: 'fade', icon: CircleDashed },
    { id: 'blurResolve', icon: ScanLine },
    { id: 'zoom', icon: ZoomIn },
    { id: 'slideUp', icon: ArrowUp },
    { id: 'slideDown', icon: ArrowDown },
    { id: 'glitchResolve', icon: Zap },
    { id: 'lightSweepReveal', icon: Sparkles },
  ];
  const maxDuration = Math.max(0.1, Math.min(5, clip.duration / 2));
  const duration = Math.min(
    edge === 'in' ? (clip.transitionInDuration ?? 1) : (clip.transitionOutDuration ?? 1),
    maxDuration,
  );
  function choose(next: ClipTransition) {
    if (next === selected) {
      setDetailsOpen(next === 'none' ? false : !detailsOpen);
      return;
    }
    setDetailsOpen(next !== 'none');
    onCommand((current) =>
      setClipPresentation(current, {
        clipId: clip.id,
        animation: 'none',
        ...(edge === 'in'
          ? { fadeIn: false, transitionIn: next }
          : { fadeOut: false, transitionOut: next }),
      }),
    );
  }
  return (
    <div className="video-studio-animation-panel">
      <ToggleGroup
        type="single"
        value={edge}
        variant="outline"
        className="grid w-full grid-cols-2"
        onValueChange={(value) => {
          if (value === '') return;
          setEdge(value as 'in' | 'out');
          setDetailsOpen(false);
        }}
      >
        <ToggleGroupItem value="in" className="w-full">
          {t('videoStudio.inspector.animations.in')}
        </ToggleGroupItem>
        <ToggleGroupItem value="out" className="w-full">
          {t('videoStudio.inspector.animations.out')}
        </ToggleGroupItem>
      </ToggleGroup>
      <div className="video-studio-animation-grid">
        {presets.map(({ id, icon: Icon }) => (
          <button
            key={id}
            type="button"
            className="video-studio-animation-card"
            aria-pressed={selected === id}
            onClick={() => {
              choose(id);
            }}
          >
            <Icon aria-hidden="true" />
            <span>{t(`videoStudio.inspector.animations.presets.${id}`)}</span>
          </button>
        ))}
      </div>
      {selected !== 'none' && detailsOpen ? (
        <div
          role="group"
          aria-label={t('videoStudio.inspector.transitionDuration')}
          className="video-studio-transition-duration"
        >
          <div className="flex items-center justify-between">
            <span>{t('videoStudio.inspector.transitionDuration')}</span>
            <span className="font-mono tabular-nums">{duration.toFixed(1)}s</span>
          </div>
          <Slider
            key={`${edge}-${String(duration)}`}
            aria-label={t('videoStudio.inspector.transitionDuration')}
            min={0.1}
            max={maxDuration}
            step={0.1}
            defaultValue={[duration]}
            onValueCommit={([next = duration]) => {
              onCommand((current) =>
                setClipPresentation(current, {
                  clipId: clip.id,
                  ...(edge === 'in'
                    ? { transitionInDuration: next }
                    : { transitionOutDuration: next }),
                }),
              );
            }}
          />
        </div>
      ) : null}
    </div>
  );
}

export function ClipInspector(props: ClipInspectorProps) {
  const { t } = useTranslation();
  const [internalTab, setInternalTab] = useState<ClipTab>('transform');
  const tab = props.activeTab ?? internalTab;
  const source = sourceOf(props.project, props.clip.sourceId);
  const hasAudio = source?.kind === 'video' && source.hasAudio !== false;
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => {
        const next = value as ClipTab;
        setInternalTab(next);
        props.onTabChange?.(next);
      }}
      className="min-w-0 gap-3"
    >
      <TabsList className="video-studio-tool-tabs">
        <TabsTrigger value="transform">{t('videoStudio.inspector.tabs.transform')}</TabsTrigger>
        <TabsTrigger value="animation">{t('videoStudio.inspector.tabs.animation')}</TabsTrigger>
        <TabsTrigger value="adjust">{t('videoStudio.inspector.tabs.adjust')}</TabsTrigger>
        {hasAudio ? (
          <TabsTrigger value="audio">{t('videoStudio.inspector.tabs.audio')}</TabsTrigger>
        ) : null}
        <TabsTrigger value="speed">{t('videoStudio.inspector.tabs.speed')}</TabsTrigger>
        <TabsTrigger value="time">{t('videoStudio.inspector.tabs.time')}</TabsTrigger>
      </TabsList>
      <TabsContent value="transform">
        <FrameControls {...props} />
      </TabsContent>
      <TabsContent value="animation">
        <AnimationControls {...props} />
      </TabsContent>
      <TabsContent value="adjust">
        <AdjustControls {...props} />
      </TabsContent>
      {hasAudio ? (
        <TabsContent value="audio">
          <AudioControls {...props} />
        </TabsContent>
      ) : null}
      <TabsContent value="speed">
        <SpeedControls {...props} />
      </TabsContent>
      <TabsContent value="time">
        <TimeControls {...props} />
      </TabsContent>
    </Tabs>
  );
}

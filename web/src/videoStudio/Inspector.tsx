import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { setProperty } from './commands';
import { ClipInspector, type ClipTab } from './Inspector_clip';
import { overlayWindow, type OverlayItem, type VideoProject } from './project';
import { Input } from '@/components/ui/input';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';

type Commit = (edit: (current: VideoProject) => VideoProject) => void;
const ANIMATIONS = ['none', 'fadeIn', 'fadeOut'] as const;
type Animation = (typeof ANIMATIONS)[number];

interface Fade {
  readonly time: number;
  readonly value: number;
}

function isFade(frame: unknown): frame is Fade {
  return (
    typeof frame === 'object' &&
    frame !== null &&
    'time' in frame &&
    'value' in frame &&
    typeof frame.time === 'number' &&
    typeof frame.value === 'number'
  );
}

function fadeFrames(value: unknown): readonly Fade[] {
  return Array.isArray(value) ? value.filter(isFade) : [];
}

function asText(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback;
}

function asAmount(value: unknown, fallback: number): number {
  return typeof value === 'number' ? value : fallback;
}

function readAmount(text: string): number | undefined {
  const value = Number(text.trim());
  return Number.isFinite(value) && value > 0 ? value : undefined;
}

function keepText(text: string): string {
  return text;
}

function readAsset(text: string): string | undefined {
  const id = text.trim();
  return id === '' ? undefined : id;
}

function fadeFor(animation: Animation, length: number): number | readonly Fade[] {
  const edge = Math.min(0.5, length / 2);
  if (animation === 'fadeIn') {
    return [
      { time: 0, value: 0 },
      { time: edge, value: 1 },
    ];
  }
  if (animation === 'fadeOut') {
    return [
      { time: length - edge, value: 1 },
      { time: length, value: 0 },
    ];
  }
  return 1;
}

function animationOf(props: Readonly<Record<string, unknown>>): Animation {
  const frames = fadeFrames(props.opacity);
  if (frames[0]?.value === 0) return 'fadeIn';
  if (frames[frames.length - 1]?.value === 0) return 'fadeOut';
  return 'none';
}

function asAnimation(value: string): Animation {
  return ANIMATIONS.find((name) => name === value) ?? 'none';
}

interface PropertyFieldProps<T> {
  readonly label: string;
  readonly value: string;
  readonly type?: 'text' | 'color';
  readonly read: (text: string) => T | undefined;
  readonly onCommit: (value: T) => void;
}

function PropertyField<T>({ label, value, type = 'text', read, onCommit }: PropertyFieldProps<T>) {
  const id = useId();
  const [text, setText] = useState(value);
  const parsed = read(text);
  return (
    <div className="flex flex-col gap-1 text-xs text-text-muted">
      <label htmlFor={id}>{label}</label>
      <Input
        id={id}
        type={type}
        value={text}
        aria-invalid={parsed === undefined}
        onChange={(event) => {
          setText(event.target.value);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter') event.currentTarget.blur();
        }}
        onBlur={() => {
          if (parsed === undefined) setText(value);
          else onCommit(parsed);
        }}
        className={type === 'color' ? 'w-20 p-1' : undefined}
      />
    </div>
  );
}

function AnimationField({
  value,
  onPick,
}: {
  readonly value: Animation;
  readonly onPick: (animation: Animation) => void;
}) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <div className="flex flex-col gap-1 text-xs text-text-muted">
      <label htmlFor={id}>{t('videoStudio.inspector.animation')}</label>
      <NativeSelect
        id={id}
        value={value}
        onChange={(event) => {
          onPick(asAnimation(event.target.value));
        }}
      >
        {ANIMATIONS.map((name) => (
          <NativeSelectOption key={name} value={name}>
            {t(`videoStudio.inspector.animations.${name}`)}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </div>
  );
}

function TextFields({
  item,
  set,
}: {
  readonly item: OverlayItem;
  readonly set: (key: string, value: unknown) => void;
}) {
  const { t } = useTranslation();
  const words = asText(item.props.text, '');
  const size = String(asAmount(item.props.fontSize, 4));
  const colour = asText(item.props.color, '#FFFFFF');
  return (
    <>
      <PropertyField
        key={`text-${item.id}-${words}`}
        label={t('videoStudio.inspector.text')}
        value={words}
        read={keepText}
        onCommit={(value) => {
          set('text', value);
        }}
      />
      <PropertyField
        key={`size-${item.id}-${size}`}
        label={t('videoStudio.inspector.size')}
        value={size}
        read={readAmount}
        onCommit={(value) => {
          set('fontSize', value);
        }}
      />
      <PropertyField
        key={`color-${item.id}-${colour}`}
        label={t('videoStudio.inspector.color')}
        type="color"
        value={colour}
        read={keepText}
        onCommit={(value) => {
          set('color', value);
        }}
      />
    </>
  );
}

function ImageFields({
  item,
  set,
}: {
  readonly item: OverlayItem;
  readonly set: (key: string, value: unknown) => void;
}) {
  const { t } = useTranslation();
  const asset = asText(item.props.assetId, '');
  const scale = String(asAmount(item.props.scale, 1));
  return (
    <>
      <PropertyField
        key={`asset-${item.id}-${asset}`}
        label={t('videoStudio.inspector.asset')}
        value={asset}
        read={readAsset}
        onCommit={(value) => {
          set('assetId', value);
        }}
      />
      <PropertyField
        key={`scale-${item.id}-${scale}`}
        label={t('videoStudio.inspector.scale')}
        value={scale}
        read={readAmount}
        onCommit={(value) => {
          set('scale', value);
        }}
      />
    </>
  );
}

function OverlayFields({
  project,
  item,
  onCommand,
}: {
  readonly project: VideoProject;
  readonly item: OverlayItem;
  readonly onCommand: Commit;
}) {
  const span = overlayWindow(project, item.anchor, item.duration);
  const set = (key: string, value: unknown) => {
    onCommand((current) => setProperty(current, { itemId: item.id, key, value }));
  };
  return (
    <>
      {item.kind === 'text' ? (
        <TextFields item={item} set={set} />
      ) : (
        <ImageFields item={item} set={set} />
      )}
      <AnimationField
        value={animationOf(item.props)}
        onPick={(animation) => {
          set('opacity', fadeFor(animation, span.end - span.start));
        }}
      />
    </>
  );
}

interface InspectorProps {
  readonly project: VideoProject;
  readonly selectedId: string | undefined;
  readonly onCommand: Commit;
  readonly activeClipTab?: ClipTab;
  readonly onClipTabChange?: (tab: ClipTab) => void;
}

export function Inspector({
  project,
  selectedId,
  onCommand,
  activeClipTab,
  onClipTabChange,
}: InspectorProps) {
  const { t } = useTranslation();
  const clip = project.video.find((item) => item.id === selectedId);
  const overlay = project.overlays
    .flatMap((track) => track.items)
    .find((item) => item.id === selectedId);
  return (
    <section
      aria-label={t('videoStudio.inspector.label')}
      className="video-studio-inspector flex flex-col gap-3 rounded-[var(--radius-md)] bg-surface-1 p-3"
    >
      {clip !== undefined ? (
        <ClipInspector
          project={project}
          clip={clip}
          onCommand={onCommand}
          {...(activeClipTab === undefined ? {} : { activeTab: activeClipTab })}
          {...(onClipTabChange === undefined ? {} : { onTabChange: onClipTabChange })}
        />
      ) : overlay !== undefined ? (
        <div className="video-studio-overlay-properties">
          <OverlayFields project={project} item={overlay} onCommand={onCommand} />
        </div>
      ) : (
        <p role="status" className="p-3 text-xs text-text-muted">
          {t('videoStudio.inspector.empty')}
        </p>
      )}
    </section>
  );
}

import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import { TimeField } from '../mediaEdit/TimeField';
import { removeRange, setMuted, setProperty, trimClip } from './commands';
import {
  clipStart,
  overlayWindow,
  type OverlayItem,
  type VideoItem,
  type VideoProject,
} from './project';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Switch } from '@/components/ui/switch';

// Inspector.tsx — what the selection is, in fields. Each one commits a command and nothing else:
// the panel holds no copy of the project, so an edit that is refused simply leaves the field
// showing what the project still says.
//
// A clip and an overlay are edited by different commands, which is why they are different
// fieldsets rather than one with holes in it: a clip's start, end and mute are `trimClip` and
// `setMuted`, an overlay's are properties VideoFlow reads, through `setProperty`.

/** The thunk the workspace applies, records in the history and translates a refusal from. */
type Commit = (edit: (current: VideoProject) => VideoProject) => void;

const ANIMATIONS = ['none', 'fadeIn', 'fadeOut'] as const;
type Animation = (typeof ANIMATIONS)[number];

/** Half a second, the length of a title fade — capped at half the overlay so a short one still
 *  reaches full strength before it has to leave again. */
const FADE = 0.5;

/** A keyframe of the fade the animation field writes. */
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

/** `props` is an untyped bag — a saved project is a file, and what is in it is not a promise. */
function fadeFrames(value: unknown): readonly Fade[] {
  return Array.isArray(value) ? value.filter(isFade) : [];
}

function asText(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback;
}

function asAmount(value: unknown, fallback: number): number {
  return typeof value === 'number' ? value : fallback;
}

/** A size or a scale: positive, and a plain number rather than whatever `Number()` would take. */
function readAmount(text: string): number | undefined {
  const value = Number(text.trim());
  return Number.isFinite(value) && value > 0 ? value : undefined;
}

/** Any string is a legal title or colour, so there is nothing here to refuse. */
function keepText(text: string): string {
  return text;
}

/** An asset id, or nothing at all: `videoflow.ts` refuses an image overlay that names no asset,
 *  and a blank one would only resolve to a URL that answers 404. */
function readAsset(text: string): string | undefined {
  const id = text.trim();
  return id === '' ? undefined : id;
}

/**
 * The opacity a fade IS. A property whose value is a list of keyframes compiles to an animation
 * (`BaseLayer.toJSON` promotes it), so the choice needs no field of its own in the model and
 * nothing in `videoflow.ts` has to learn about it. `none` is VisualLayer's own default: a layer
 * at full strength.
 */
function fadeFor(animation: Animation, length: number): number | readonly Fade[] {
  const edge = Math.min(FADE, length / 2);
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

/** Which fade the keyframes already there describe — the field reads back what it wrote. */
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

/**
 * A property as an input: it keeps what is being typed, commits on Enter or blur, and restores a
 * value it cannot read instead of letting it reach the project. That is `TimeField`'s discipline
 * over a value space that is not a timecode — the two time fields below ARE TimeField.
 *
 * The draft is state, so a caller remounts the field with a key carrying BOTH the item and the
 * incoming value — TimeField states the same contract. Without the item in that key, typing into
 * one overlay's field and then selecting another of the same kind would leave the first one's
 * words on screen and commit them into the second; without the value, an undo would not reach it.
 */
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

interface RangeFieldsProps {
  readonly project: VideoProject;
  readonly clip: VideoItem;
  readonly onCommand: Commit;
}

/**
 * Take a stretch out of the middle of this clip and close the lane over it. The spec's cycle-1
 * list names it beside trim, split, mute and reorder, and it is the only one of the five with no
 * control: reaching it as split, split and remove is three undo steps and a different gesture.
 *
 * The two fields read SOURCE time, the frame the Start and End fields above already show, because
 * that is what the operator is looking at while typing these. `removeRange` reads PROJECT time, so
 * each is converted once by where the clip sits in the lane — the same conversion a dragged handle
 * makes through `trimArgsFromSpan`, in the other direction.
 *
 * The removal waits for the button. A time field commits on blur, and a blur on the way to the
 * second field must not take a stretch of the film with it.
 */
function RangeFields({ project, clip, onCommand }: RangeFieldsProps) {
  const { t } = useTranslation();
  const [from, setFrom] = useState(clip.sourceStart);
  const [to, setTo] = useState(clip.sourceStart + clip.duration);
  return (
    <>
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
    </>
  );
}

interface ClipFieldsProps {
  readonly project: VideoProject;
  readonly clip: VideoItem;
  readonly onCommand: Commit;
}

/**
 * Where the clip starts and stops in the source it plays, and whether it is heard.
 *
 * Both fields show SOURCE time and `trimClip` counts from where the clip already starts in that
 * source, so each commits the difference. Adding the clip's own start back would count it twice —
 * once in the argument and once in the command — and move the clip twice as far as it was asked.
 *
 * Each is remounted on the clip's id as well as on its value: two clips can legitimately begin at
 * the same point of the same source, and a key made of the value alone would let a time typed
 * against one of them survive the switch to the other and commit there.
 */
function ClipFields({ project, clip, onCommand }: ClipFieldsProps) {
  const { t } = useTranslation();
  const id = useId();
  const end = clip.sourceStart + clip.duration;
  return (
    <>
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
      {/* Re-seeded whenever the clip it measures moves: the range is this clip's own bounds to
          narrow, and bounds held from before an edit would name a stretch that has gone. */}
      <RangeFields
        key={`range-${clip.id}-${formatTimecode(clip.sourceStart)}-${formatTimecode(end)}`}
        project={project}
        clip={clip}
        onCommand={onCommand}
      />
    </>
  );
}

interface AnimationFieldProps {
  readonly value: Animation;
  readonly onPick: (animation: Animation) => void;
}

function AnimationField({ value, onPick }: AnimationFieldProps) {
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

interface OverlayFieldsProps {
  readonly project: VideoProject;
  readonly item: OverlayItem;
  readonly onCommand: Commit;
}

interface KindFieldsProps {
  readonly item: OverlayItem;
  readonly set: (key: string, value: unknown) => void;
}

/** A title: the words, how big they are, and what colour. */
function TextFields({ item, set }: KindFieldsProps) {
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
        onCommit={(next) => {
          set('text', next);
        }}
      />
      <PropertyField
        key={`size-${item.id}-${size}`}
        label={t('videoStudio.inspector.size')}
        value={size}
        read={readAmount}
        onCommit={(next) => {
          set('fontSize', next);
        }}
      />
      <PropertyField
        key={`color-${item.id}-${colour}`}
        label={t('videoStudio.inspector.color')}
        type="color"
        value={colour}
        read={keepText}
        onCommit={(next) => {
          set('color', next);
        }}
      />
    </>
  );
}

/**
 * A still: which asset it shows, and how big.
 *
 * `props.assetId` is the ONLY thing in the model that can name an image — `OverlayItem` carries
 * no source of its own and `videoflow.ts` reads the id straight out of the props — so without
 * this field an image overlay could never be given one, or have a wrong one corrected.
 */
function ImageFields({ item, set }: KindFieldsProps) {
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
        onCommit={(next) => {
          set('assetId', next);
        }}
      />
      <PropertyField
        key={`scale-${item.id}-${scale}`}
        label={t('videoStudio.inspector.scale')}
        value={scale}
        read={readAmount}
        onCommit={(next) => {
          set('scale', next);
        }}
      />
    </>
  );
}

/**
 * An overlay's properties, which are VideoFlow's own. The fade is measured against the window the
 * renderer really gets — an overlay clipped by the end of the clip it hangs on is shorter than
 * its own duration, and a fade-out past that edge would never arrive.
 */
function OverlayFields({ project, item, onCommand }: OverlayFieldsProps) {
  const span = overlayWindow(project, item.anchor, item.duration);
  function set(key: string, value: unknown) {
    onCommand((current) => setProperty(current, { itemId: item.id, key, value }));
  }
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
}

export function Inspector({ project, selectedId, onCommand }: InspectorProps) {
  const { t } = useTranslation();
  const clip = project.video.find((item) => item.id === selectedId);
  const overlay = project.overlays
    .flatMap((track) => track.items)
    .find((item) => item.id === selectedId);
  return (
    <section
      aria-label={t('videoStudio.inspector.label')}
      className="flex flex-col gap-3 rounded-[var(--radius-md)] bg-surface-1 p-3"
    >
      {clip !== undefined ? (
        <ClipFields project={project} clip={clip} onCommand={onCommand} />
      ) : overlay !== undefined ? (
        <OverlayFields project={project} item={overlay} onCommand={onCommand} />
      ) : (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.inspector.empty')}
        </p>
      )}
    </section>
  );
}

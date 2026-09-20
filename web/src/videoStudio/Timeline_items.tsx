import { useItem, type Span } from 'dnd-timeline';
import type { KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import type { OverlayItem, VideoItem } from './project';
import { atMilli, clamp, stepOnArrow, TOUCH_FLOOR, type TrimSpan } from './timelineView';

// Timeline_items.tsx — what sits on a lane: a clip with its two trim handles, and an overlay that
// only selects, because cycle 1 has no command that moves one.
//
// A handle's pointer drag is not here. dnd-timeline owns it — the press lands inside the item's
// resize band and the release reaches the shell as `onResizeEnd` — so nothing in this file writes
// to the project. A keystroke is its own release, and that is the only edit these items emit.

/** Announced on the clip itself, so the reorder is discoverable and not folklore. */
const REORDER_KEYS = 'Alt+ArrowLeft Alt+ArrowRight';

interface HandleProps {
  readonly frame: number;
  /** Where the handle should now sit in the source, already inside its own bounds. */
  readonly onSet: (at: number) => void;
  readonly label: string;
  readonly value: number;
  readonly min: number;
  readonly max: number;
  readonly side: 'start' | 'end';
}

/**
 * A trim handle: a slider that reads where the clip sits in the source it plays. A step is clamped
 * to the handle's own bounds BEFORE it notifies, the way mediaEdit/VideoTimeline does it — a
 * handle at the source's start that asked for a negative position would only collect a refusal,
 * and an arrow held at the end would collect one per keystroke.
 */
function Handle({ label, value, min, max, side, frame, onSet }: HandleProps) {
  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label={label}
      aria-valuemin={atMilli(min)}
      aria-valuemax={atMilli(max)}
      aria-valuenow={atMilli(value)}
      aria-valuetext={formatTimecode(value)}
      data-required-touch-target
      onKeyDown={stepOnArrow({
        frame,
        onStep: (delta) => {
          const at = clamp(value + delta, min, max);
          if (at !== value) onSet(at);
        },
      })}
      style={{
        position: 'absolute',
        top: 0,
        bottom: 0,
        minWidth: TOUCH_FLOOR,
        width: TOUCH_FLOOR,
        [side === 'start' ? 'left' : 'right']: 0,
        transform: `translateX(${side === 'start' ? '-50%' : '50%'})`,
      }}
      className="z-10 flex cursor-ew-resize touch-none items-center justify-center focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      <span aria-hidden="true" className="h-2/3 w-1.5 rounded-full bg-accent" />
    </div>
  );
}

/** dnd-kit's draggable attributes, named through the hook so nothing imports it directly. */
type ItemAttributes = ReturnType<typeof useItem>['attributes'];

interface ItemButtonProps {
  readonly label: string;
  readonly length: number;
  readonly selected: boolean;
  readonly idle: string;
  readonly onSelect: () => void;
  readonly onKeyDown?: ((event: KeyboardEvent<HTMLButtonElement>) => void) | undefined;
  readonly keyShortcuts?: string | undefined;
  readonly activatorRef?: ((element: HTMLElement | null) => void) | undefined;
  readonly attributes?: ItemAttributes | undefined;
}

/** What both lanes put on the timeline: a block that says how long it is and selects on click. */
function ItemButton({
  label,
  length,
  selected,
  idle,
  onSelect,
  onKeyDown,
  keyShortcuts,
  activatorRef,
  attributes,
}: ItemButtonProps) {
  return (
    <button
      {...attributes}
      ref={activatorRef}
      type="button"
      aria-label={label}
      aria-current={selected ? 'true' : undefined}
      aria-keyshortcuts={keyShortcuts}
      data-required-touch-target
      style={{ minHeight: TOUCH_FLOOR }}
      // The PRESS selects, and the click is kept for the keyboard. Measured in Chrome on
      // 2026-09-20: a click on a clip selected nothing. dnd-kit's PointerSensor is configured
      // here with no activation distance, so a plain press already counts as the start of a
      // drag — `handleStart` adds a capturing document `click` listener that calls
      // `stopPropagation` (@dnd-kit/core core.esm.js:1504) and the button's onClick never runs.
      // Selecting on pointerdown is also what the gesture means: what you press is what you are
      // about to drag. Enter and Space still arrive as clicks, with no drag before them.
      onPointerDown={onSelect}
      onClick={onSelect}
      onKeyDown={onKeyDown}
      className={`flex h-full w-full items-center overflow-hidden rounded-[var(--radius-md)] border px-2 text-left text-xs ${
        selected ? 'border-accent bg-surface-2' : idle
      }`}
    >
      <span className="truncate">{formatTimecode(length)}</span>
    </button>
  );
}

interface ClipItemProps {
  readonly clip: VideoItem;
  readonly index: number;
  readonly count: number;
  readonly start: number;
  readonly sourceEnd: number;
  readonly frame: number;
  readonly selected: boolean;
  readonly onSelect: (id: string) => void;
  readonly onMove: (clipId: string, toIndex: number) => void;
  readonly onTrim: (clipId: string, args: TrimSpan) => void;
}

export function ClipItem({
  clip,
  index,
  count,
  start,
  sourceEnd,
  frame,
  selected,
  onSelect,
  onMove,
  onTrim,
}: ClipItemProps) {
  const { t } = useTranslation();
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, itemStyle, itemContentStyle } =
    useItem({
      id: clip.id,
      span: { start, end: start + clip.duration },
      resizeHandleWidth: TOUCH_FLOOR,
    });
  const position = { index: index + 1 };
  return (
    <div
      ref={setNodeRef}
      style={itemStyle}
      onPointerDown={listeners.onPointerDown}
      onPointerMove={listeners.onPointerMove}
    >
      <div style={itemContentStyle}>
        <ItemButton
          label={t('videoStudio.timeline.clip', position)}
          length={clip.duration}
          selected={selected}
          idle="border-border bg-surface-3"
          attributes={attributes}
          activatorRef={setActivatorNodeRef}
          onSelect={() => {
            onSelect(clip.id);
          }}
          keyShortcuts={REORDER_KEYS}
          // The keyboard's answer to the drag, because a drag no mouse can make is unusable. It
          // takes Alt: a plain arrow navigates, and a navigation key that silently reorders the
          // film is not a thing anyone asked for.
          onKeyDown={(event) => {
            const back = event.key === 'ArrowLeft';
            if (!event.altKey || (!back && event.key !== 'ArrowRight')) return;
            event.preventDefault();
            const toIndex = index + (back ? -1 : 1);
            if (toIndex < 0 || toIndex >= count) return;
            onMove(clip.id, toIndex);
          }}
        />
      </div>
      <Handle
        side="start"
        label={t('videoStudio.timeline.trimStart', position)}
        value={clip.sourceStart}
        min={0}
        max={clip.sourceStart + clip.duration - frame}
        frame={frame}
        // The handle speaks in source time and `trimClip` counts from the clip's own start, so
        // the announced position converts back by subtracting it — never by adding it again.
        onSet={(at) => {
          onTrim(clip.id, { start: at - clip.sourceStart, end: clip.duration });
        }}
      />
      <Handle
        side="end"
        label={t('videoStudio.timeline.trimEnd', position)}
        value={clip.sourceStart + clip.duration}
        min={clip.sourceStart + frame}
        max={sourceEnd}
        frame={frame}
        onSet={(at) => {
          onTrim(clip.id, { start: 0, end: at - clip.sourceStart });
        }}
      />
    </div>
  );
}

interface OverlayItemProps {
  readonly item: OverlayItem;
  readonly index: number;
  readonly span: Span;
  readonly selected: boolean;
  readonly onSelect: (id: string) => void;
}

/** An overlay rides the clip it hangs on, and cycle 1 has no command to move it: it selects only. */
export function OverlayItemView({ item, index, span, selected, onSelect }: OverlayItemProps) {
  const { t } = useTranslation();
  const { setNodeRef, itemStyle, itemContentStyle } = useItem({
    id: item.id,
    span,
    disabled: true,
  });
  const position = { index: index + 1 };
  const label =
    item.kind === 'text'
      ? t('videoStudio.timeline.overlayText', position)
      : t('videoStudio.timeline.overlayImage', position);
  return (
    <div ref={setNodeRef} style={itemStyle}>
      <div style={itemContentStyle}>
        <ItemButton
          label={label}
          length={span.end - span.start}
          selected={selected}
          idle="border-border bg-surface-4"
          onSelect={() => {
            onSelect(item.id);
          }}
        />
      </div>
    </div>
  );
}

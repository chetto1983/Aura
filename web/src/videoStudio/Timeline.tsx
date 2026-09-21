import {
  TimelineContext,
  useRow,
  useTimelineContext,
  useTimelineMonitor,
  type DragEndEvent,
  type ResizeEndEvent,
  type Span,
} from 'dnd-timeline';
import { Blend, Minus, Plus } from 'lucide-react';
import { useState, type PointerEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import { moveClip, trimClip } from './commands';
import {
  clipStart,
  clipStarts,
  junctionDurationAt,
  overlayWindow,
  projectDuration,
  type VideoProject,
  type ClipJunction,
} from './project';
import { ClipItem, OverlayItemView } from './Timeline_items';
import {
  atMilli,
  clamp,
  formatRulerTime,
  insertIndexFor,
  MIN_VISIBLE,
  offsetOf,
  rulerMarks,
  rulerStep,
  sourceEndOf,
  stepOnArrow,
  TOUCH_FLOOR,
  trimArgsFromSpan,
  zoomedRange,
  type TrimSpan,
} from './timelineView';
import { Button } from '@/components/ui/button';

// Timeline.tsx — the lanes, the ruler, the playhead, the zoom and the two gestures that edit:
// drag a clip to another place in the sequence, drag a handle to change what it plays.
//
// Nothing here owns the project. A gesture in flight is view state — dnd-timeline animates the
// item it is moving and tells us nothing until it is let go — and only the release hands the shell
// a command thunk. The shell applies it as one undo step and translates a refusal; this
// component never catches one.
//
// The video lane is a sequence, so a drop is an INSERT: the drop's time picks a place among the
// clips the dragged one has left, never a free position and never an overlap.

interface LaneProps {
  readonly id: string;
  readonly label: string;
  readonly droppable: boolean;
  readonly children: ReactNode;
}

function Lane({ id, label, droppable, children }: LaneProps) {
  const { setNodeRef, rowWrapperStyle, rowStyle } = useRow({
    id,
    disabled: !droppable,
  });
  return (
    <div
      role="group"
      aria-label={label}
      data-lane={id}
      style={{ ...rowWrapperStyle, width: '100%' }}
    >
      <span className="sr-only">{label}</span>
      <div
        ref={setNodeRef}
        style={{ ...rowStyle, minHeight: TOUCH_FLOOR + 8 }}
        className="video-studio-lane relative border-t border-border py-1"
      >
        {children}
      </div>
    </div>
  );
}

interface ZoomButtonProps {
  readonly label: string;
  readonly direction: 'in' | 'out';
  readonly onPress: () => void;
}

function ZoomButton({ label, direction, onPress }: ZoomButtonProps) {
  return (
    <Button
      type="button"
      aria-label={label}
      data-required-touch-target
      variant="outline"
      size="icon"
      style={{ minHeight: TOUCH_FLOOR, minWidth: TOUCH_FLOOR }}
      onClick={onPress}
      className="video-studio-zoom-button rounded-[var(--radius-md)] border border-border text-sm text-fg-muted hover:text-fg focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {direction === 'in' ? <Plus aria-hidden="true" /> : <Minus aria-hidden="true" />}
    </Button>
  );
}

interface ScrubberProps {
  readonly range: Span;
  readonly playhead: number;
  readonly total: number;
  readonly frame: number;
  readonly onScrub: (time: number) => void;
}

/** The ruler doubles as the playhead's slider: marks to read, and the scrub that moves it. */
function Scrubber({ range, playhead, total, frame, onScrub }: ScrubberProps) {
  const { t } = useTranslation();
  function timeAt(event: PointerEvent<HTMLDivElement>): number {
    const box = event.currentTarget.getBoundingClientRect();
    if (box.width === 0) return range.start;
    const ratio = clamp((event.clientX - box.left) / box.width, 0, 1);
    return atMilli(range.start + ratio * (range.end - range.start));
  }
  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label={t('videoStudio.timeline.playhead')}
      aria-valuemin={0}
      aria-valuemax={atMilli(total)}
      aria-valuenow={atMilli(playhead)}
      aria-valuetext={formatTimecode(playhead)}
      onKeyDown={stepOnArrow({
        frame,
        onStep: (delta) => {
          onScrub(atMilli(clamp(playhead + delta, 0, total)));
        },
      })}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        onScrub(timeAt(event));
      }}
      onPointerMove={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) onScrub(timeAt(event));
      }}
      style={{ minHeight: TOUCH_FLOOR }}
      className="video-studio-ruler relative flex-1 touch-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {rulerMarks(range, rulerStep(range.end - range.start)).map((mark) => (
        <span
          key={mark}
          data-testid="timeline-mark"
          data-time={mark}
          style={{ left: offsetOf(mark, range) }}
          className="video-studio-ruler-mark absolute top-0 border-l border-border pt-1 pl-1 text-[10px] text-fg-muted tabular-nums"
        >
          {formatRulerTime(mark)}
        </span>
      ))}
    </div>
  );
}

interface TimelineProps {
  readonly project: VideoProject;
  readonly selectedId: string | undefined;
  readonly playhead: number;
  readonly onSelect: (id: string) => void;
  readonly onCommand: (edit: (current: VideoProject) => VideoProject) => void;
  readonly onScrub: (time: number) => void;
  readonly selectedJunction?: ClipJunction | undefined;
  readonly onSelectJunction?: ((junction: ClipJunction) => void) | undefined;
}

interface LanesProps extends Omit<TimelineProps, 'onCommand'> {
  readonly range: Span;
  readonly total: number;
  readonly onZoom: (factor: number) => void;
  readonly onMove: (clipId: string, toIndex: number) => void;
  readonly onTrim: (clipId: string, args: TrimSpan) => void;
}

/**
 * Everything inside the timeline element, which is where the bag lives. The drop's span is the
 * library's answer; the sequence's answer to that span is `insertIndexFor`.
 */
function Lanes({
  project,
  selectedId,
  playhead,
  range,
  total,
  onSelect,
  onScrub,
  onZoom,
  onMove,
  onTrim,
  selectedJunction,
  onSelectJunction,
}: LanesProps) {
  const { t } = useTranslation();
  const { style, setTimelineRef } = useTimelineContext();
  useTimelineMonitor({
    onDragEnd: (event) => {
      // The span is the library's to compute: the item's own strategy knows the timeline's scale
      // and whatever snapping it was given, and rebuilding it from a pixel delta would drift from
      // both. `useTimelineMonitor` types the callback with dnd-kit's event, whose `data` is the
      // untyped bag; dnd-timeline's own DragEndEvent is that same event with the bag named, and
      // this is the one line that says so.
      const drag = event as DragEndEvent;
      const span = drag.active.data.current.getSpanFromDragEvent?.(drag);
      if (span === null || span === undefined) return;
      const clipId = String(drag.active.id);
      onMove(clipId, insertIndexFor(project, clipId, span.start));
    },
  });
  const starts = clipStarts(project);
  const frame = 1 / project.fps;
  return (
    <div
      ref={setTimelineRef}
      role="group"
      aria-label={t('videoStudio.timeline.label')}
      style={style}
      className="video-studio-timeline w-full rounded-[var(--radius-md)] bg-surface-1"
    >
      <div className="video-studio-ruler-row">
        <Scrubber range={range} playhead={playhead} total={total} frame={frame} onScrub={onScrub} />
        <div className="video-studio-timeline-zoom">
          <ZoomButton
            label={t('videoStudio.timeline.zoomOut')}
            direction="out"
            onPress={() => {
              onZoom(2);
            }}
          />
          <ZoomButton
            label={t('videoStudio.timeline.zoomIn')}
            direction="in"
            onPress={() => {
              onZoom(0.5);
            }}
          />
        </div>
      </div>
      {project.overlays.map((track, index) => (
        <Lane
          key={track.id}
          id={track.id}
          label={t('videoStudio.timeline.overlayLane', { index: index + 1 })}
          droppable={false}
        >
          {track.items.map((item, position) => (
            <OverlayItemView
              key={item.id}
              item={item}
              index={position}
              span={overlayWindow(project, item.anchor, item.duration)}
              selected={item.id === selectedId}
              onSelect={onSelect}
            />
          ))}
        </Lane>
      ))}
      <Lane id="video" label={t('videoStudio.timeline.videoLane')} droppable>
        {project.video.map((clip, index) => (
          <ClipItem
            key={clip.id}
            clip={clip}
            source={project.sources.find((source) => source.id === clip.sourceId)}
            index={index}
            count={project.video.length}
            start={starts[index] ?? 0}
            sourceEnd={sourceEndOf(project, clip)}
            frame={frame}
            selected={clip.id === selectedId}
            onSelect={onSelect}
            onMove={onMove}
            onTrim={onTrim}
          />
        ))}
        {project.video.slice(1).map((incoming, offset) => {
          const toIndex = offset + 1;
          const outgoing = project.video[toIndex - 1];
          if (outgoing === undefined) return null;
          const duration = junctionDurationAt(project, toIndex);
          const junction = { fromClipId: outgoing.id, toClipId: incoming.id };
          const selected =
            selectedJunction?.fromClipId === outgoing.id &&
            selectedJunction.toClipId === incoming.id;
          return (
            <button
              key={`${outgoing.id}-${incoming.id}`}
              type="button"
              data-required-touch-target
              data-active={duration > 0 ? 'true' : 'false'}
              aria-pressed={selected}
              aria-label={t('videoStudio.timeline.transition', {
                index: toIndex,
                next: toIndex + 1,
              })}
              className="video-studio-junction"
              style={{ left: offsetOf((starts[toIndex] ?? 0) + duration / 2, range) }}
              onPointerDown={(event) => {
                event.stopPropagation();
              }}
              onClick={(event) => {
                event.stopPropagation();
                onSelectJunction?.(junction);
              }}
            >
              <Blend aria-hidden="true" />
            </button>
          );
        })}
      </Lane>
      <div
        aria-hidden="true"
        style={{ position: 'absolute', inset: 0 }}
        className="pointer-events-none"
      >
        <div
          data-testid="timeline-playhead"
          style={{ left: offsetOf(playhead, range) }}
          className="video-studio-playhead absolute top-0 bottom-0 w-px bg-accent"
        />
      </div>
    </div>
  );
}

export function Timeline({
  project,
  selectedId,
  playhead,
  onSelect,
  onCommand,
  onScrub,
  selectedJunction,
  onSelectJunction,
}: TimelineProps) {
  const total = Math.max(projectDuration(project), MIN_VISIBLE);
  // Null is "fit the project". A range held from before is re-clamped against the project as it is
  // now, so a lane that grew or shrank under the view never leaves it pointing outside.
  const [view, setView] = useState<Span | null>(null);
  const range = view === null ? { start: 0, end: total } : zoomedRange(view, 1, total);
  function onMove(clipId: string, toIndex: number) {
    onCommand((current) => moveClip(current, { clipId, toIndex }));
  }
  function onTrim(clipId: string, args: TrimSpan) {
    onCommand((current) => trimClip(current, { clipId, ...args }));
  }
  return (
    <TimelineContext
      range={range}
      sidebarWidth={0}
      onRangeChanged={(update) => {
        setView(update(range));
      }}
      onResizeEnd={(event: ResizeEndEvent) => {
        const clipId = String(event.active.id);
        const from = clipStart(project, clipId);
        const clip = project.video.find((item) => item.id === clipId);
        const span = event.active.data.current.getSpanFromResizeEvent?.(event);
        if (from === undefined || clip === undefined || span === null || span === undefined) return;
        onTrim(clipId, trimArgsFromSpan(from, span, clip.speed));
      }}
    >
      <Lanes
        project={project}
        selectedId={selectedId}
        playhead={playhead}
        range={range}
        total={total}
        onSelect={onSelect}
        onScrub={onScrub}
        onZoom={(factor) => {
          setView(zoomedRange(range, factor, total));
        }}
        onMove={onMove}
        onTrim={onTrim}
        selectedJunction={selectedJunction}
        onSelectJunction={onSelectJunction}
      />
    </TimelineContext>
  );
}

import {
  TimelineContext,
  useRow,
  useTimelineContext,
  useTimelineMonitor,
  type DragEndEvent,
  type ResizeEndEvent,
  type Span,
} from 'dnd-timeline';
import { useState, type PointerEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import { moveClip, trimClip } from './commands';
import {
  clipStart,
  clipStarts,
  overlayWindow,
  projectDuration,
  type VideoProject,
} from './project';
import { ClipItem, OverlayItemView } from './Timeline_items';
import {
  atMilli,
  clamp,
  insertIndexFor,
  MIN_VISIBLE,
  offsetOf,
  rulerMarks,
  rulerStep,
  SIDEBAR_WIDTH,
  sourceEndOf,
  stepOnArrow,
  TOUCH_FLOOR,
  trimArgsFromSpan,
  zoomedRange,
  type TrimSpan,
} from './timelineView';

// Timeline.tsx — the lanes, the ruler, the playhead, the zoom and the two gestures that edit:
// drag a clip to another place in the sequence, drag a handle to change what it plays.
//
// Nothing here owns the project. A gesture in flight is view state — dnd-timeline animates the
// item it is moving and tells us nothing until it is let go — and only the release hands the shell
// a command thunk. The shell applies it, decides whether it is a transaction, and translates a
// refusal; this component never catches one.
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
  const { setNodeRef, rowWrapperStyle, rowSidebarStyle, rowStyle } = useRow({
    id,
    disabled: !droppable,
  });
  return (
    <div style={{ ...rowWrapperStyle, width: '100%' }}>
      <div style={rowSidebarStyle} className="items-center px-2 text-xs text-fg-muted">
        {label}
      </div>
      <div
        ref={setNodeRef}
        style={{ ...rowStyle, minHeight: TOUCH_FLOOR + 8 }}
        className="relative border-t border-border py-1"
      >
        {children}
      </div>
    </div>
  );
}

interface ZoomButtonProps {
  readonly label: string;
  readonly glyph: string;
  readonly onPress: () => void;
}

function ZoomButton({ label, glyph, onPress }: ZoomButtonProps) {
  return (
    <button
      type="button"
      aria-label={label}
      data-required-touch-target
      style={{ minHeight: TOUCH_FLOOR, minWidth: TOUCH_FLOOR }}
      onClick={onPress}
      className="rounded-[var(--radius-md)] border border-border text-sm text-fg-muted hover:text-fg focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      <span aria-hidden="true">{glyph}</span>
    </button>
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
      className="relative flex-1 touch-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {rulerMarks(range, rulerStep(range.end - range.start)).map((mark) => (
        <span
          key={mark}
          data-testid="timeline-mark"
          data-time={mark}
          style={{ left: offsetOf(mark, range) }}
          className="absolute top-0 border-l border-border pt-1 pl-1 text-[10px] text-fg-muted tabular-nums"
        >
          {formatTimecode(mark)}
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
      className="w-full rounded-[var(--radius-md)] bg-surface-1"
    >
      <div style={{ display: 'flex', width: '100%' }}>
        <div style={{ width: SIDEBAR_WIDTH }} className="flex items-center gap-1 px-2">
          <ZoomButton
            label={t('videoStudio.timeline.zoomOut')}
            glyph="−"
            onPress={() => {
              onZoom(2);
            }}
          />
          <ZoomButton
            label={t('videoStudio.timeline.zoomIn')}
            glyph="+"
            onPress={() => {
              onZoom(0.5);
            }}
          />
        </div>
        <Scrubber range={range} playhead={playhead} total={total} frame={frame} onScrub={onScrub} />
      </div>
      <Lane id="video" label={t('videoStudio.timeline.videoLane')} droppable>
        {project.video.map((clip, index) => (
          <ClipItem
            key={clip.id}
            clip={clip}
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
      </Lane>
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
      <div
        aria-hidden="true"
        style={{ position: 'absolute', top: 0, bottom: 0, left: SIDEBAR_WIDTH, right: 0 }}
        className="pointer-events-none"
      >
        <div
          data-testid="timeline-playhead"
          style={{ left: offsetOf(playhead, range) }}
          className="absolute top-0 bottom-0 w-px bg-accent"
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
      sidebarWidth={SIDEBAR_WIDTH}
      onRangeChanged={(update) => {
        setView(update(range));
      }}
      onResizeEnd={(event: ResizeEndEvent) => {
        const clipId = String(event.active.id);
        const from = clipStart(project, clipId);
        const span = event.active.data.current.getSpanFromResizeEvent?.(event);
        if (from === undefined || span === null || span === undefined) return;
        onTrim(clipId, trimArgsFromSpan(from, span));
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
      />
    </TimelineContext>
  );
}

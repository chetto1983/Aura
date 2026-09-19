import { useEffect, useRef, type KeyboardEvent, type PointerEvent } from 'react';
import { MIN_SPAN, trimLength } from './editRules';
import { formatTimecode } from './timecode';

// VideoTimeline — the filmstrip with two handles (start, end), after Adobe Express and 123apps.
// The handles are sliders for assistive tech and the keyboard: arrows move a tenth of a second,
// Shift+arrows a whole second.

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function tenth(value: number): number {
  return Math.round(value * 10) / 10;
}

// A handle's bound snaps inward to the tenth grid, never past the span it guards: an end at 7.36
// lets the start reach 7.2, not 7.3. The epsilon absorbs float noise such as 0.3 - 0.1.
function tenthBelow(value: number): number {
  return Math.floor(value * 10 + 1e-9) / 10;
}

function tenthAbove(value: number): number {
  return Math.ceil(value * 10 - 1e-9) / 10;
}

function Frame({ source }: { readonly source: CanvasImageSource }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    const context = canvas?.getContext('2d');
    if (!canvas || !context) return;
    context.drawImage(source, 0, 0, canvas.width, canvas.height);
  }, [source]);
  return (
    <canvas
      ref={ref}
      width={160}
      height={90}
      aria-hidden="true"
      className="h-full min-w-0 flex-1"
    />
  );
}

interface HandleProps {
  readonly label: string;
  readonly value: number;
  readonly min: number;
  readonly max: number;
  readonly position: string;
  readonly onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void;
  readonly onPointerDown: (event: PointerEvent<HTMLDivElement>) => void;
  readonly onPointerMove: (event: PointerEvent<HTMLDivElement>) => void;
}

function Handle({ label, value, min, max, position, ...handlers }: HandleProps) {
  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label={label}
      aria-valuemin={tenth(min)}
      aria-valuemax={tenth(max)}
      aria-valuenow={tenth(value)}
      aria-valuetext={formatTimecode(value)}
      style={{ left: position }}
      className="absolute inset-y-0 z-10 flex w-11 -translate-x-1/2 cursor-ew-resize touch-none items-center justify-center rounded-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      {...handlers}
    >
      <span aria-hidden="true" className="h-full w-2.5 rounded-sm bg-accent" />
    </div>
  );
}

interface VideoTimelineProps {
  readonly duration: number;
  readonly start: number;
  readonly end: number;
  readonly frames: readonly CanvasImageSource[];
  readonly onChange: (start: number, end: number) => void;
  readonly startLabel: string;
  readonly endLabel: string;
}

export function VideoTimeline({
  duration,
  start,
  end,
  frames,
  onChange,
  startLabel,
  endLabel,
}: VideoTimelineProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  // A clip shorter than one step keeps its handles apart by the whole clip, so every min stays
  // below its max.
  const length = trimLength(duration);
  const span = Math.min(MIN_SPAN, length);
  const startMax = Math.max(0, tenthBelow(end - span));
  const endMin = Math.min(tenthAbove(start + span), length);
  const setStart = (value: number) => {
    onChange(clamp(tenth(value), 0, startMax), end);
  };
  const setEnd = (value: number) => {
    onChange(start, clamp(tenth(value), endMin, length));
  };

  function timeAt(clientX: number): number {
    const box = trackRef.current?.getBoundingClientRect();
    if (box === undefined || box.width === 0) return 0;
    return clamp((clientX - box.left) / box.width, 0, 1) * length;
  }

  function keys(set: (value: number) => void, value: number) {
    return (event: KeyboardEvent<HTMLDivElement>) => {
      const step = event.shiftKey ? 1 : 0.1;
      const back = event.key === 'ArrowLeft' || event.key === 'ArrowDown';
      const forward = event.key === 'ArrowRight' || event.key === 'ArrowUp';
      if (!back && !forward) return;
      event.preventDefault();
      set(value + (back ? -step : step));
    };
  }

  function grab(set: (value: number) => void) {
    return (event: PointerEvent<HTMLDivElement>) => {
      event.currentTarget.setPointerCapture(event.pointerId);
      set(timeAt(event.clientX));
    };
  }

  function follow(set: (value: number) => void) {
    return (event: PointerEvent<HTMLDivElement>) => {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) set(timeAt(event.clientX));
    };
  }

  const percent = (value: number) => (length > 0 ? clamp(value / length, 0, 1) * 100 : 0);
  const at = (value: number) => `${String(percent(value))}%`;
  const fromEnd = `${String(100 - percent(end))}%`;

  return (
    <div
      ref={trackRef}
      data-testid="video-timeline"
      className="relative h-14 w-full touch-none overflow-hidden rounded-[var(--radius-md)] bg-surface-3 select-none"
    >
      <div className="flex h-full">
        {frames.map((frame, index) => (
          // Thumbnails are positional: the index is their identity.
          <Frame key={index} source={frame} />
        ))}
      </div>
      <div
        aria-hidden="true"
        className="absolute inset-y-0 left-0 bg-bg/70"
        style={{ width: at(start) }}
      />
      <div
        aria-hidden="true"
        className="absolute inset-y-0 right-0 bg-bg/70"
        style={{ width: fromEnd }}
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-0 border-y-2 border-accent"
        style={{ left: at(start), right: fromEnd }}
      />
      <Handle
        label={startLabel}
        value={start}
        min={0}
        max={startMax}
        position={at(start)}
        onKeyDown={keys(setStart, start)}
        onPointerDown={grab(setStart)}
        onPointerMove={follow(setStart)}
      />
      <Handle
        label={endLabel}
        value={end}
        min={endMin}
        max={length}
        position={at(end)}
        onKeyDown={keys(setEnd, end)}
        onPointerDown={grab(setEnd)}
        onPointerMove={follow(setEnd)}
      />
    </div>
  );
}

import { TimelineContext, useItem, useRow, useTimelineContext, type Span } from 'dnd-timeline';
import { useEffect, useRef, useState, type PointerEvent, type ReactNode, type RefObject } from 'react';
import { createRoot } from 'react-dom/client';
import WaveSurfer from 'wavesurfer.js';
import EnvelopePlugin from 'wavesurfer.js/plugins/envelope';

const SECONDS = 300;
// 'svg' is the brief's guard (any press on the envelope's SVG). 'point' narrows it to the drag
// handles: the SVG spans the whole waveform (width/height 100%, absolute, z-index 4), and with
// `dragLine` off (its default) the <ellipse> points are the only thing in it that drags.
const guard = new URLSearchParams(location.search).get('guard') ?? 'none';
const HANDLE = guard === 'point' ? SVGEllipseElement : SVGElement;
// 'content' is the brief's layout (the waveform inside `itemContentStyle`); 'span' gives it the
// item's full box instead.
const layout = new URLSearchParams(location.search).get('layout') ?? 'content';
// The envelope freezes its viewBox at creation (`preserveAspectRatio="none"`), so after a zoom
// its strokes stretch sideways with the SVG. 'fixed' pins them through the parts the plugin
// exposes on its shadow tree.
if (new URLSearchParams(location.search).get('stroke') === 'fixed') {
  const style = document.createElement('style');
  style.textContent =
    '[data-testid="waveform"] > div::part(polyline), [data-testid="waveform"] > div::part(envelope-circle) { vector-effect: non-scaling-stroke; }';
  document.head.appendChild(style);
}
const log: Record<string, unknown>[] = [];
Object.assign(window, { s2Log: log });
window.addEventListener(
  'pointerdown',
  (event) => {
    const first = event.composedPath()[0];
    log.push({ event: 'press', on: first instanceof Element ? first.tagName : String(first) });
  },
  { capture: true },
);

function peaks(count: number): Float32Array {
  const out = new Float32Array(count);
  for (let i = 0; i < count; i += 1) out[i] = 0.2 + 0.6 * Math.abs(Math.sin(i / 37));
  return out;
}

// The envelope freezes its viewBox at the wrapper's width when it is created, and a dnd-timeline
// item has no width until the timeline has measured itself: created on mount, the viewBox is
// `0 0 1 48` for good. So the waveform waits for its host to have a width.
function useLaidOut(host: RefObject<HTMLDivElement | null>): boolean {
  const [laidOut, setLaidOut] = useState(false);
  useEffect(() => {
    if (host.current === null) return undefined;
    const observer = new ResizeObserver(([entry]) => setLaidOut((entry?.contentRect.width ?? 0) > 0));
    observer.observe(host.current);
    return () => observer.disconnect();
  }, [host]);
  return laidOut;
}

function Waveform({ duration }: { duration: number }) {
  const host = useRef<HTMLDivElement>(null);
  const laidOut = useLaidOut(host);
  useEffect(() => {
    if (host.current === null || !laidOut) return undefined;
    const started = performance.now();
    const envelope = EnvelopePlugin.create({
      points: [
        { time: 60, volume: 1 },
        { time: 120, volume: 0.3 },
        { time: 240, volume: 0.3 },
      ],
      lineColor: '#f5b041',
      dragPointSize: 14,
    });
    const ws = WaveSurfer.create({
      container: host.current,
      height: 48,
      peaks: [peaks(4000)],
      duration,
      interact: false,
      cursorWidth: 0,
      waveColor: '#2e9e8f',
      normalize: true,
      plugins: [envelope],
    });
    const geometry = () => {
      const svg = ws.getWrapper().querySelector('svg');
      return {
        wrapperWidth: ws.getWrapper().clientWidth,
        hostWidth: host.current?.clientWidth,
        viewBox: svg?.getAttribute('viewBox'),
        svgWidth: svg?.clientWidth,
        rx: svg?.querySelector('ellipse')?.getAttribute('rx'),
      };
    };
    log.push({ event: 'geometry-at-create', ...geometry() });
    ws.on('redrawcomplete', () =>
      log.push({ event: 'redrawcomplete', ms: performance.now() - started, at: performance.now(), ...geometry() }),
    );
    envelope.on('points-change', (points) =>
      log.push({ event: 'points-change', points: points.map((p) => [Math.round(p.time), Number(p.volume.toFixed(2))]) }),
    );
    return () => ws.destroy();
  }, [duration, laidOut]);
  // The guard under test: a press that lands on the envelope never reaches dnd-kit. wavesurfer
  // renders into an open shadow root, so outside it `event.target` is retargeted to the host; only
  // the composed path still names the element the press landed on.
  const stop = (event: PointerEvent) => {
    if (guard !== 'none' && event.nativeEvent.composedPath().some((node) => node instanceof HANDLE)) {
      event.stopPropagation();
    }
  };
  return <div ref={host} data-testid="waveform" onPointerDown={stop} style={{ width: '100%' }} />;
}

function AudioItem({ span }: { span: Span }) {
  const { setNodeRef, attributes, listeners, itemStyle, itemContentStyle } = useItem({
    id: 'music',
    span,
    resizeHandleWidth: 44,
  });
  const body = (
    <div {...attributes} data-testid="item" style={{ width: '100%', background: '#1d2b33', borderRadius: 6 }}>
      <Waveform duration={span.end - span.start} />
    </div>
  );
  return (
    <div ref={setNodeRef} style={itemStyle} onPointerDown={listeners.onPointerDown} onPointerMove={listeners.onPointerMove}>
      {layout === 'span' ? (
        // The item box is the whole span; the timeline's overflow clips what lies outside the range.
        <div style={{ position: 'absolute', inset: 0 }}>{body}</div>
      ) : (
        // dnd-timeline pads its content box down to the part of the span inside the range.
        <div style={itemContentStyle}>{body}</div>
      )}
    </div>
  );
}

function Lane({ children }: { children: ReactNode }) {
  const { setNodeRef, rowWrapperStyle, rowStyle } = useRow({ id: 'audio-lane' });
  return (
    <div style={{ ...rowWrapperStyle, width: '100%' }}>
      <div ref={setNodeRef} style={{ ...rowStyle, minHeight: 64 }}>
        {children}
      </div>
    </div>
  );
}

function Lanes({ span }: { span: Span }) {
  const { style, setTimelineRef } = useTimelineContext();
  return (
    <div ref={setTimelineRef} style={{ ...style, width: 1100 }}>
      <Lane>
        <AudioItem span={span} />
      </Lane>
    </div>
  );
}

function App() {
  const [span, setSpan] = useState<Span>({ start: 20, end: 20 + SECONDS });
  const [range, setRange] = useState({ start: 0, end: 400 });
  // A zoom: the same item, twice as wide, with the wavesurfer instance kept.
  useEffect(() => {
    Object.assign(window, {
      s2Zoom: (start: number, end: number) => {
        log.push({ event: 'zoom', start, end, at: performance.now() });
        setRange({ start, end });
      },
    });
  }, []);
  return (
    <TimelineContext
      range={range}
      sidebarWidth={0}
      onRangeChanged={() => undefined}
      onResizeEnd={() => log.push({ event: 'item-resize-end' })}
      onDragStart={() => log.push({ event: 'item-drag-start' })}
      onDragEnd={(event) => {
        const next = event.active.data.current.getSpanFromDragEvent?.(event);
        log.push({ event: 'item-drag-end', start: next?.start ?? null });
        if (next !== null && next !== undefined) setSpan(next);
      }}
    >
      <Lanes span={span} />
    </TimelineContext>
  );
}

createRoot(document.getElementById('root') as HTMLElement).render(<App />);

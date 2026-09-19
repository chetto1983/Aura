import { useCallback, useRef, useState } from 'react';
import { TimelineContext, useItem, useRow, useTimelineContext } from 'dnd-timeline';
import { ops } from './editor/ops.js';
import { endOf, nameOf, useEditor, usePreview } from './editor/useEditor.js';
import './timeline.css';

// dnd-timeline works in abstract numbers; this shell uses milliseconds. Snap grid 250 ms.
const GRID_MS = 250;
const SIDEBAR = 72;

function Item({ layer }) {
  const span = { start: layer.settings.startTime * 1000, end: endOf(layer) * 1000 };
  const { setNodeRef, attributes, listeners, itemStyle, itemContentStyle } = useItem({ id: layer.id, span, resizeHandleWidth: 16 });
  return (
    <div ref={setNodeRef} style={itemStyle} {...listeners} {...attributes} data-testid={`item-${nameOf(layer)}`}>
      <div style={itemContentStyle}>
        <div className={`clip clip-${layer.type}`}>{nameOf(layer)}</div>
      </div>
    </div>
  );
}

function Row({ id, label, children }) {
  const { setNodeRef, rowWrapperStyle, rowStyle, rowSidebarStyle } = useRow({ id });
  return (
    <div style={{ ...rowWrapperStyle, minHeight: 36 }} className="track">
      <div style={{ ...rowSidebarStyle, width: SIDEBAR }} className="track-label">{label}</div>
      <div ref={setNodeRef} style={rowStyle}>{children}</div>
    </div>
  );
}

function Tracks({ project, frame, fps, onSeek }) {
  const { setTimelineRef, style, valueToPixels, range, getValueFromScreenX } = useTimelineContext();
  const x = SIDEBAR + valueToPixels((frame / fps) * 1000 - range.start);
  return (
    <div ref={setTimelineRef} style={style} className="timeline" data-testid="timeline">
      <div className="ruler" data-testid="ruler" style={{ marginLeft: SIDEBAR }} onPointerDown={(e) => onSeek(Math.max(0, getValueFromScreenX(e.clientX)) / 1000)}>
        {Array.from({ length: Math.ceil((range.end - range.start) / 1000) + 1 }, (_, s) => (
          <span key={s} style={{ left: valueToPixels(s * 1000 + Math.ceil(range.start / 1000) * 1000 - range.start) }}>{s + Math.ceil(range.start / 1000)}s</span>
        ))}
      </div>
      {project.layers.map((l) => (
        <Row key={l.id} id={`row-${l.id}`} label={nameOf(l)}>
          <Item layer={l} />
        </Row>
      ))}
      <div className="playhead" data-testid="playhead" style={{ left: x }} />
    </div>
  );
}

export default function DndTimelineLab() {
  const { history, project } = useEditor();
  const host = useRef(null);
  const [frame, setFrame] = useState(0);
  const player = usePreview(host, project, setFrame);
  const [range, setRange] = useState({ start: 0, end: 6000 });

  const seek = useCallback((sec) => player.current?.seek(Math.round(sec * (project?.fps ?? 24))), [player, project]);

  const onDragEnd = useCallback((event) => {
    const span = event.active.data.current.getSpanFromDragEvent?.(event);
    if (span) history.commit('move', (d) => ops.move(d, event.active.id, span.start / 1000));
  }, [history]);

  const onResizeEnd = useCallback((event) => {
    const span = event.active.data.current.getSpanFromResizeEvent?.(event);
    if (!span) return;
    const edge = event.direction === 'start' ? 'start' : 'end';
    history.commit('trim', (d) => ops.trim(d, event.active.id, edge, (edge === 'start' ? span.start : span.end) / 1000));
  }, [history]);

  const zoom = (factor) => setRange((r) => ({ start: r.start, end: r.start + Math.max(1000, (r.end - r.start) * factor) }));
  window.__spike = window.__spike ?? {};
  window.__spike.range = range;

  return (
    <section className="lab">
      <div ref={host} className="player" data-testid="preview" />
      <div className="row">
        <button onClick={() => player.current?.play()}>Play</button>
        <button onClick={() => player.current?.stop()}>Stop</button>
        <button onClick={() => history?.undo()} data-testid="undo">Annulla</button>
        <button onClick={() => history?.redo()} data-testid="redo">Ripeti</button>
        <button onClick={() => zoom(0.5)} data-testid="zoom-in">Zoom +</button>
        <button onClick={() => zoom(2)} data-testid="zoom-out">Zoom −</button>
        <span>{(frame / (project?.fps ?? 24)).toFixed(2)} s</span>
      </div>
      {project && (
        <TimelineContext
          range={range}
          onRangeChanged={setRange}
          onDragEnd={onDragEnd}
          onResizeEnd={onResizeEnd}
          rangeGridSizeDefinition={GRID_MS}
          sidebarWidth={SIDEBAR}
        >
          <Tracks project={project} frame={frame} fps={project.fps} onSeek={seek} />
        </TimelineContext>
      )}
    </section>
  );
}

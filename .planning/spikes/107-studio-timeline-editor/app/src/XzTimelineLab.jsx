import { useCallback, useMemo, useRef, useState } from 'react';
import { Timeline } from '@xzdarcy/react-timeline-editor';
import '@xzdarcy/react-timeline-editor/dist/react-timeline-editor.css';
import { ops } from './editor/ops.js';
import { endOf, nameOf, useEditor, usePreview } from './editor/useEditor.js';
import './timeline.css';

// react-timeline-editor works in seconds. Its model (rows of actions) is derived from the VideoJSON on
// every render and never stored: the editor's own onChange copy is ignored, our history is the truth.
const EFFECTS = { video: { id: 'video' }, text: { id: 'text' }, image: { id: 'image' } };

export default function XzTimelineLab() {
  const { history, project } = useEditor();
  const host = useRef(null);
  const timeline = useRef(null);
  const [frame, setFrame] = useState(0);
  const [scaleWidth, setScaleWidth] = useState(160);
  const fps = project?.fps ?? 24;
  const onFrame = useCallback((f) => { setFrame(f); timeline.current?.setTime(f / fps); }, [fps]);
  const player = usePreview(host, project, onFrame);
  const seek = useCallback((sec) => player.current?.seek(Math.round(sec * fps)), [player, fps]);

  const rows = useMemo(() => (project?.layers ?? []).map((l) => ({
    id: `row-${l.id}`,
    actions: [{ id: l.id, start: l.settings.startTime, end: endOf(l), effectId: l.type }],
  })), [project]);

  window.__spike = window.__spike ?? {};
  window.__spike.xzScaleWidth = scaleWidth;
  window.__spike.xzCursor = () => timeline.current?.getTime();

  return (
    <section className="lab">
      <div ref={host} className="player" data-testid="preview" />
      <div className="row">
        <button onClick={() => player.current?.play()}>Play</button>
        <button onClick={() => player.current?.stop()}>Stop</button>
        <button onClick={() => history?.undo()} data-testid="undo">Annulla</button>
        <button onClick={() => history?.redo()} data-testid="redo">Ripeti</button>
        <button onClick={() => setScaleWidth((w) => w * 2)} data-testid="zoom-in">Zoom +</button>
        <button onClick={() => setScaleWidth((w) => Math.max(40, w / 2))} data-testid="zoom-out">Zoom −</button>
        <span>{(frame / fps).toFixed(2)} s</span>
      </div>
      {project && (
        <Timeline
          ref={timeline}
          editorData={rows}
          effects={EFFECTS}
          scale={1}
          scaleSplitCount={4}
          scaleWidth={scaleWidth}
          startLeft={12}
          rowHeight={36}
          gridSnap
          dragLine
          autoScroll
          style={{ width: '100%', height: 200 }}
          getActionRender={(action) => {
            const l = project.layers.find((x) => x.id === action.id);
            return <div className={`clip clip-${l.type}`} data-testid={`item-${nameOf(l)}`}>{nameOf(l)}</div>;
          }}
          onActionMoveEnd={({ action, start }) => history.commit('move', (d) => ops.move(d, action.id, start))}
          onActionResizeEnd={({ action, start, end, dir }) => history.commit('trim', (d) => ops.trim(d, action.id, dir === 'left' ? 'start' : 'end', dir === 'left' ? start : end))}
          onCursorDrag={seek}
          onClickTimeArea={(time) => { seek(time); return undefined; }}
          onChange={() => false}
        />
      )}
    </section>
  );
}

import DomRenderer from '@videoflow/renderer-dom';
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { setProperty } from './commands';
import { clipAt, overlayWindow, type OverlayItem, type VideoProject } from './project';
import { atMilli, clamp, TOUCH_FLOOR } from './timelineView';
import { toVideoJSON, withLocalFonts, type FontLoadingRenderer } from './videoflow';

// Stage.tsx — the picture: VideoFlow's DOM renderer on a host element, and over it the one
// gesture that edits by dragging instead of by typing.
//
// The renderer belongs to the host: built with it, destroyed with it. The project is compiled by
// `videoflow.ts` — this file never composes a layer — and reloaded whenever it changes; the
// playhead is a seek. Everything inside the picture is the renderer's and its shadow root is not
// reached into; everything that can be grabbed is ordinary DOM over it, which is why there is no
// canvas library here.
//
// The drag is the editor's only live-feedback path: the box follows the pointer in local state
// and the RELEASE is the command. Writing on every move would put a hundred entries in the
// history for one gesture, and undo would have to walk back out of all of them.

/** A point on the frame as VideoFlow's `position` states it: fractions of width and height,
 *  `[0, 0]` top-left and `[1, 1]` bottom-right (VisualLayer.propertiesDefinition). */
type Position = readonly [number, number];

/** VisualLayer's own default for `position`: the middle of the frame. */
const CENTRE: Position = [0.5, 0.5];
/** An arrow nudges a hundredth of the frame, shift a tenth. */
const FINE_STEP = 0.01;
const COARSE_STEP = 0.1;
const ARROWS: Readonly<Record<string, Position>> = {
  ArrowLeft: [-1, 0],
  ArrowRight: [1, 0],
  ArrowUp: [0, -1],
  ArrowDown: [0, 1],
};

function isPosition(value: unknown): value is Position {
  return (
    Array.isArray(value) &&
    value.length >= 2 &&
    typeof value[0] === 'number' &&
    typeof value[1] === 'number'
  );
}

/** Where an overlay sits, defaulted and clamped: a saved project is a file anyone can edit, and a
 *  box outside the picture is a box nobody can grab back. */
function positionOf(props: Readonly<Record<string, unknown>>): Position {
  const value = props.position;
  return isPosition(value) ? [clamp(value[0], 0, 1), clamp(value[1], 0, 1)] : CENTRE;
}

/** Move a point and keep it on the frame — the clamp the drag and the arrows share. */
function moved(at: Position, dx: number, dy: number): Position {
  return [clamp(at[0] + dx, 0, 1), clamp(at[1] + dy, 0, 1)];
}

function percent(fraction: number): string {
  return `${String(atMilli(fraction * 100))}%`;
}

/** The overlay the box belongs to: the selected one, while it is on screen at `time`. A clip is
 *  never boxed — it has no position of its own, and its edits are the timeline's. */
function boxedOverlay(
  project: VideoProject,
  selectedId: string | undefined,
  time: number,
): OverlayItem | undefined {
  const item = project.overlays
    .flatMap((track) => track.items)
    .find((candidate) => candidate.id === selectedId);
  if (item === undefined) return undefined;
  const span = overlayWindow(project, item.anchor, item.duration);
  return time >= span.start && time < span.end ? item : undefined;
}

/**
 * A renderer that loads its fonts from our own origin, and the only place one is constructed.
 *
 * The cast is over a single declaration: `loadedFonts` is a real public field on DomRenderer
 * (`DomRenderer.js`) that its `.d.ts` marks private, which is what spike 107 §6 measured when it
 * replaced the stock `loadFont` on both renderers. Construction and override are one expression,
 * so no caller can reach `loadVideo` with the off-origin loader still in place.
 */
function localFontRenderer(host: HTMLElement): DomRenderer {
  const renderer = new DomRenderer(host);
  withLocalFonts(renderer as unknown as FontLoadingRenderer);
  return renderer;
}

interface SelectionBoxProps {
  readonly label: string;
  readonly at: Position;
  readonly onMove: (to: Position) => void;
}

/** The handle over the selected overlay. It is a plain positioned button, so it is focusable,
 *  grabbable at the coarse-pointer floor, and movable with the arrows — a drag no mouse can make
 *  would be unusable. */
function SelectionBox({ label, at, onMove }: SelectionBoxProps) {
  const [dragged, setDragged] = useState<Position | undefined>(undefined);
  const from = useRef<{ readonly x: number; readonly y: number; readonly at: Position }>(undefined);
  const shown = dragged ?? at;

  function drag(event: PointerEvent<HTMLButtonElement>) {
    const origin = from.current;
    const box = event.currentTarget.parentElement?.getBoundingClientRect();
    // A stage with no width is one nothing has laid out yet: there is no fraction to compute.
    if (origin === undefined || box === undefined || box.width === 0 || box.height === 0) return;
    setDragged(
      moved(
        origin.at,
        (event.clientX - origin.x) / box.width,
        (event.clientY - origin.y) / box.height,
      ),
    );
  }

  function keys(event: KeyboardEvent<HTMLButtonElement>) {
    const arrow = ARROWS[event.key];
    if (arrow === undefined) return;
    event.preventDefault();
    const step = event.shiftKey ? COARSE_STEP : FINE_STEP;
    onMove(moved(at, arrow[0] * step, arrow[1] * step));
  }

  return (
    <button
      type="button"
      aria-label={label}
      data-required-touch-target
      style={{
        position: 'absolute',
        left: percent(shown[0]),
        top: percent(shown[1]),
        minWidth: TOUCH_FLOOR,
        minHeight: TOUCH_FLOOR,
        transform: 'translate(-50%, -50%)',
      }}
      className="cursor-move touch-none rounded-[var(--radius-md)] border-2 border-accent bg-accent/10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        from.current = { x: event.clientX, y: event.clientY, at };
      }}
      onPointerMove={drag}
      onPointerUp={() => {
        from.current = undefined;
        if (dragged !== undefined) onMove(dragged);
        setDragged(undefined);
      }}
      onKeyDown={keys}
    />
  );
}

interface StageProps {
  readonly project: VideoProject;
  readonly time: number;
  readonly selectedId: string | undefined;
  readonly onSelect: (id: string) => void;
  readonly onCommand: (edit: (current: VideoProject) => VideoProject) => void;
}

export function Stage({ project, time, selectedId, onSelect, onCommand }: StageProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [renderer, setRenderer] = useState<DomRenderer | null>(null);
  // The same renderer again, once its project is really on screen. A seek before the load would
  // ask for a frame of a video the renderer has not been given.
  const [loaded, setLoaded] = useState<DomRenderer | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const host = hostRef.current;
    if (host === null) return undefined;
    const instance = localFontRenderer(host);
    setRenderer(instance);
    return () => {
      setRenderer(null);
      instance.destroy();
    };
  }, []);

  useEffect(() => {
    if (renderer === null) return undefined;
    let live = true;
    void toVideoJSON(project, { assetUrl })
      .then((json) => (live ? renderer.loadVideo(json) : undefined))
      .then(
        () => {
          if (!live) return;
          setLoaded(renderer);
          setFailed(false);
        },
        () => {
          // A source the browser will not decode, or a project pointing at bytes that are gone:
          // the picture stays dark and says so. The refusal channel belongs to the workspace, and
          // a preview that threw through React would take the editor with it.
          if (live) setFailed(true);
        },
      );
    return () => {
      live = false;
      setLoaded(null);
    };
  }, [renderer, project, assetUrl]);

  useEffect(() => {
    if (loaded === null) return;
    loaded.seek(Math.round(time * project.fps)).catch(() => {
      setFailed(true);
    });
  }, [loaded, time, project.fps]);

  const item = boxedOverlay(project, selectedId, time);
  return (
    <div
      data-testid="video-stage"
      style={{ aspectRatio: `${String(project.size.width)} / ${String(project.size.height)}` }}
      className="relative w-full overflow-hidden rounded-[var(--radius-md)] bg-black"
    >
      <div ref={hostRef} className="absolute inset-0" />
      <button
        type="button"
        aria-label={t('videoStudio.stage.picture')}
        className="absolute inset-0 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        onClick={() => {
          const clip = clipAt(project, time);
          if (clip !== undefined) onSelect(clip.id);
        }}
      />
      {item === undefined ? null : (
        <SelectionBox
          label={t('videoStudio.stage.selection')}
          at={positionOf(item.props)}
          onMove={(to) => {
            onCommand((current) =>
              setProperty(current, { itemId: item.id, key: 'position', value: to }),
            );
          }}
        />
      )}
      {failed ? (
        <p
          role="status"
          className="absolute inset-x-0 bottom-0 bg-surface-1/90 p-2 text-center text-xs text-danger"
        >
          {t('videoStudio.stage.failed')}
        </p>
      ) : null}
    </div>
  );
}

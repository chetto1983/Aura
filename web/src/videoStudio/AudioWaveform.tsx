import {
  useEffect,
  useEffectEvent,
  useImperativeHandle,
  useRef,
  useState,
  type PointerEvent,
  type Ref,
  type RefObject,
} from 'react';
import WaveSurfer from 'wavesurfer.js';
import EnvelopePlugin from 'wavesurfer.js/plugins/envelope';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { waveformSamples, WAVEFORM_RATE } from './audioDecode';
import { envelopeFromView, envelopeView, peaksOf, sameView } from './audioView';
import type { EnvelopePoint } from './project';

// AudioWaveform.tsx — a sound's waveform inside its lane item and, while it is selected, its volume
// envelope: points to drag, a double-click to add one, a drag off the item to remove one. S2 set the
// rules this follows (spikes/video-studio-audio/FINDINGS.md): it is created only once its host has
// a width, because the envelope freezes its viewBox at creation; it fills the item box, not
// dnd-timeline's content box; a press on a point is kept from dnd-kit, found by the composed path
// because wavesurfer draws in a shadow root; and its strokes are pinned through ::part (CSS).
// Every other press goes through to the sound's button (the layer takes none), so the double-click
// that adds a point reaches the plugin through `addPointAt`, not through its own SVG.

/** Peaks drawn per second of sound: finer than any zoom the lane offers shows. */
const PEAKS_PER_SECOND = 100;

/** Whether the host has been laid out yet. Under jsdom it never is — its ResizeObserver is the
 *  no-op polyfill in src/test/setup.ts, which never reports — so nothing is decoded or drawn
 *  in a test that does not stand in an observer of its own. */
function useHasWidth(host: RefObject<HTMLDivElement | null>): boolean {
  const [hasWidth, setHasWidth] = useState(false);
  useEffect(() => {
    const element = host.current;
    if (element === null) return undefined;
    const observer = new ResizeObserver(([entry]) => {
      setHasWidth((entry?.contentRect.width ?? 0) > 0);
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [host]);
  return hasWidth;
}

export interface WaveformHandle {
  /** Adds an envelope point where a double-click landed, as the plugin's own double-click would:
   *  across is time, down is gain. Nothing while no envelope is shown. */
  readonly addPointAt: (clientX: number, clientY: number) => void;
}

interface AudioWaveformProps {
  readonly ref?: Ref<WaveformHandle>;
  readonly assetId: string;
  readonly sourceStart: number;
  /** Source seconds the lane shows: the window, through the speed, cut at the film's end. */
  readonly visible: number;
  readonly envelope: readonly EnvelopePoint[] | undefined;
  readonly selected: boolean;
  readonly onEnvelope: (points: readonly EnvelopePoint[]) => void;
}

type Envelope = ReturnType<typeof EnvelopePlugin.create>;

export function AudioWaveform({
  ref,
  assetId,
  sourceStart,
  visible,
  envelope,
  selected,
  onEnvelope,
}: AudioWaveformProps) {
  const { assetUrl } = useAssetSource();
  const host = useRef<HTMLDivElement | null>(null);
  const hasWidth = useHasWidth(host);
  const [samples, setSamples] = useState<Float32Array>();
  const plugin = useRef<Envelope>(undefined);
  // Read with the newest props when the effects below run, without rebuilding the waveform for every
  // envelope edit: a rebuild under a drag would take the point from under the pointer.
  const currentView = useEffectEvent(() => envelopeView(envelope, visible));
  // The plugin reports every change it is told of too: setPoints below ends in the same
  // `points-change` as a drag, 200 ms later (envelope.esm.js). Points the project already holds are
  // that echo, not an edit — committing it would add the edges a trimmed envelope lacks as a new
  // step and wipe the redo.
  const report = useEffectEvent((points: readonly { time: number; volume: number }[]) => {
    if (!sameView(points, currentView())) onEnvelope(envelopeFromView(points, envelope, visible));
  });

  useEffect(() => {
    if (!hasWidth) return undefined;
    let live = true;
    waveformSamples(assetUrl(assetId)).then(
      (decoded) => {
        if (live) setSamples(decoded);
      },
      () => {
        // Undecodable here: the lane still shows the sound's block, which is all it can show.
      },
    );
    return () => {
      live = false;
    };
  }, [hasWidth, assetUrl, assetId]);

  useEffect(() => {
    const container = host.current;
    if (container === null || samples === undefined || visible <= 0) return undefined;
    const envelopePlugin = selected
      ? EnvelopePlugin.create({
          points: currentView(),
          dragPointSize: 14,
        })
      : undefined;
    const surfer = WaveSurfer.create({
      container,
      height: Math.max(1, container.clientHeight),
      peaks: [
        peaksOf(
          samples,
          WAVEFORM_RATE,
          sourceStart,
          sourceStart + visible,
          Math.max(1, Math.round(visible * PEAKS_PER_SECOND)),
        ),
      ],
      duration: visible,
      interact: false,
      cursorWidth: 0,
      normalize: true,
      waveColor: getComputedStyle(container).color,
      plugins: envelopePlugin === undefined ? [] : [envelopePlugin],
    });
    envelopePlugin?.on('points-change', (points) => {
      report(points);
    });
    plugin.current = envelopePlugin;
    return () => {
      plugin.current = undefined;
      surfer.destroy();
    };
  }, [samples, sourceStart, visible, selected]);

  // An envelope changed elsewhere — undo, a split — reaches the points already drawn. The one this
  // plugin just sent back is what it already shows, so a drag is never reset under the pointer.
  const viewKey = JSON.stringify(envelopeView(envelope, visible));
  useEffect(() => {
    const shown = plugin.current;
    const wanted = currentView();
    if (shown !== undefined && !sameView(shown.getPoints(), wanted)) shown.setPoints(wanted);
  }, [viewKey]);

  useImperativeHandle(
    ref,
    () => ({
      addPointAt: (clientX, clientY) => {
        const shown = plugin.current;
        if (shown === undefined || host.current === null) return;
        const box = host.current.getBoundingClientRect();
        shown.addPoint({
          time: ((clientX - box.left) / box.width) * visible,
          volume: 1 - (clientY - box.top) / box.height,
        });
      },
    }),
    [visible],
  );

  const keepPointsFromDrag = (event: PointerEvent<HTMLDivElement>) => {
    const onPoint = event.nativeEvent
      .composedPath()
      .some((node) => node instanceof Element && node.localName === 'ellipse');
    if (onPoint) event.stopPropagation();
  };
  return (
    // A pointer guard, not a control: the points inside are the controls, and dnd-kit must not see
    // their presses (S2).
    <div
      ref={host}
      data-testid="sound-waveform"
      className="video-studio-waveform"
      onPointerDown={keepPointsFromDrag}
    />
  );
}

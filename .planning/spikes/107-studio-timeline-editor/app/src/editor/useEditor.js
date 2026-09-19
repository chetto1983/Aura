import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import DomRenderer from '@videoflow/renderer-dom';
import { buildComposition } from '../project.js';
import { useLocalFonts } from '../fonts.js';
import { createHistory } from './history.js';
import { log } from '../log.js';

// One history per lab, exposed to Playwright as window.__spike.editor.
export function useEditor() {
  const [history, setHistory] = useState(null);
  useEffect(() => {
    buildComposition().then((json) => {
      const h = createHistory(json);
      window.__spike = window.__spike ?? {};
      window.__spike.editor = h;
      setHistory(h);
    });
  }, []);
  const project = useSyncExternalStore(
    (fn) => history?.subscribe(fn) ?? (() => {}),
    () => history?.state ?? null,
  );
  return { history, project };
}

// Keeps a DomRenderer in step with the project. The first load is loadVideo; every later change is
// replayed with the renderer's incremental primitives (add/remove/updateLayer, updateVideo), which
// re-render only the current frame. The renderer mutates what it is given, so it gets clones.
export function usePreview(hostRef, project, onFrame) {
  const player = useRef(null);
  const shown = useRef(null);
  useEffect(() => {
    const p = useLocalFonts(new DomRenderer(hostRef.current));
    player.current = p;
    window.__spike = window.__spike ?? {};
    window.__spike.player = p;
    return () => { p.destroy(); player.current = null; shown.current = null; };
  }, [hostRef]);
  useEffect(() => { if (player.current) player.current.onFrame = onFrame; }, [onFrame]);
  useEffect(() => {
    const p = player.current;
    if (!p || !project) return;
    const prev = shown.current;
    shown.current = project;
    const t0 = performance.now();
    (async () => {
      if (!prev) {
        await p.loadVideo(structuredClone(project));
        window.__spike.previewReady = true;
      } else {
        const before = new Map(prev.layers.map((l) => [l.id, l]));
        const after = new Map(project.layers.map((l) => [l.id, l]));
        for (const id of before.keys()) if (!after.has(id)) await p.removeLayer(id);
        for (const [i, l] of project.layers.entries()) {
          const old = before.get(l.id);
          if (!old) await p.addLayer(structuredClone(l), i);
          else if (old !== l) await p.updateLayer(l.id, structuredClone({ settings: l.settings, properties: l.properties, animations: l.animations, track: l.track ?? null }));
        }
        if (prev.duration !== project.duration) await p.updateVideo({ duration: project.duration });
      }
      window.__spike.lastSyncMs = +(performance.now() - t0).toFixed(1);
    })().catch((e) => log('error', `preview sync: ${e.message}`));
  }, [project]);
  return player;
}

export const nameOf = (l) => l.settings.name ?? l.id;
export const endOf = (l) => l.settings.startTime + l.settings.sourceDuration / Math.abs(l.settings.speed ?? 1);

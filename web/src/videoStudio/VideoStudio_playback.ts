import { useEffect, useState } from 'react';

// VideoStudio_playback.ts — the editor's clock: where the playhead is, whether it runs, and what
// moves it — a tick while playing, and a seek. Playing stops on its own at the film's end, and a
// play pressed there starts again from the beginning.

/** How often a running playhead advances: often enough for the preview to follow it smoothly. */
const TICK_MS = 50;

export function usePlayback(duration: number) {
  const [playhead, setPlayhead] = useState(0);
  const [playing, setPlaying] = useState(false);

  useEffect(() => {
    if (!playing || duration <= 0) return undefined;
    let last = performance.now();
    const timer = window.setInterval(() => {
      const now = performance.now();
      const elapsed = (now - last) / 1000;
      last = now;
      setPlayhead((current) => {
        const next = Math.min(duration, current + elapsed);
        if (next >= duration) setPlaying(false);
        return next;
      });
    }, TICK_MS);
    return () => {
      window.clearInterval(timer);
    };
  }, [duration, playing]);

  function seek(time: number) {
    setPlaying(false);
    setPlayhead(Math.min(Math.max(time, 0), duration));
  }

  function toggle() {
    if (playhead >= duration) setPlayhead(0);
    setPlaying((current) => !current);
  }

  return { playhead, playing, seek, toggle };
}

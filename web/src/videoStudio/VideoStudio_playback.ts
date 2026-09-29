import { useCallback, useRef, useState } from 'react';

// The renderer owns the picture and audio clock. The transport records its
// painted frames, so a slow audio mix cannot make the displayed time run ahead.

export function usePlayback(duration: number) {
  const [playhead, setPlayhead] = useState(0);
  const [playing, setPlaying] = useState(false);
  const lastFrameTime = useRef(0);

  const onFrame = useCallback(
    (time: number) => {
      const next = Math.min(Math.max(time, 0), duration);
      if (next < lastFrameTime.current && lastFrameTime.current - next > duration / 2) {
        lastFrameTime.current = duration;
        setPlayhead(duration);
        setPlaying(false);
        return;
      }
      lastFrameTime.current = next;
      setPlayhead(next);
    },
    [duration],
  );

  function seek(time: number) {
    const next = Math.min(Math.max(time, 0), duration);
    setPlaying(false);
    lastFrameTime.current = next;
    setPlayhead(next);
  }

  function toggle() {
    if (duration <= 0) return;
    if (playhead >= duration) {
      lastFrameTime.current = 0;
      setPlayhead(0);
    }
    setPlaying((current) => !current);
  }

  return { playhead, playing, seek, toggle, onFrame };
}

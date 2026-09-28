import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlayback } from '../VideoStudio_playback';

// The editor's clock, on fake time: it runs, stops at the film's end, restarts from the beginning
// when played there, and a seek stops it inside the film.

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('usePlayback', () => {
  it('advances while playing, and stops at the end of the film', () => {
    const { result } = renderHook(() => usePlayback(1));
    act(() => {
      result.current.toggle();
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(result.current.playing).toBe(true);
    expect(result.current.playhead).toBeCloseTo(0.5, 1);
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.playhead).toBe(1);
    expect(result.current.playing).toBe(false);
  });

  it('starts again from the beginning when played at the end', () => {
    const { result } = renderHook(() => usePlayback(2));
    act(() => {
      result.current.seek(2);
    });
    act(() => {
      result.current.toggle();
    });
    expect(result.current.playhead).toBe(0);
    expect(result.current.playing).toBe(true);
  });

  it('stops on a seek, and keeps the playhead inside the film', () => {
    const { result } = renderHook(() => usePlayback(3));
    act(() => {
      result.current.toggle();
    });
    act(() => {
      result.current.seek(9);
    });
    expect(result.current.playing).toBe(false);
    expect(result.current.playhead).toBe(3);
    act(() => {
      result.current.seek(-1);
    });
    expect(result.current.playhead).toBe(0);
  });

  it('does not run a film with no length', () => {
    const { result } = renderHook(() => usePlayback(0));
    act(() => {
      result.current.toggle();
      vi.advanceTimersByTime(500);
    });
    expect(result.current.playhead).toBe(0);
  });
});

import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { usePlayback } from '../VideoStudio_playback';

// The renderer paints the frames and mixes the sound; these checks keep the
// transport on that one clock, including its wrap at the end of a film.

describe('usePlayback', () => {
  it('follows rendered frames and stops when the renderer wraps at the end', () => {
    const { result } = renderHook(() => usePlayback(1));
    act(() => {
      result.current.toggle();
    });
    act(() => {
      result.current.onFrame(0.5);
    });
    expect(result.current.playing).toBe(true);
    expect(result.current.playhead).toBeCloseTo(0.5, 1);
    act(() => {
      result.current.onFrame(0.98);
      result.current.onFrame(0);
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
      result.current.onFrame(0.5);
    });
    expect(result.current.playhead).toBe(0);
  });
});

import { act, render, renderHook, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DuckingStatus } from '../analysisStatus';
import type { AudioItem, VideoProject } from '../project';
import { useDuckingListener, type DuckingListening } from '../VideoStudio_ducking';

// Ducking listening by itself (operator, 2026-09-28: "Listen by itself"): what it listens to, when,
// one sound at a time, and what it writes — with the speech detector stood in for.

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
  }),
}));

type Speech = (readonly [number, number])[];
const hearing = vi.hoisted(() => ({
  detect: vi.fn<(url: string, signal?: AbortSignal) => Promise<(readonly [number, number])[]>>(),
}));
vi.mock('../audioSpeech', () => ({ detectSpeech: hearing.detect }));

function voice(id: string, sourceId: string): AudioItem {
  return {
    id,
    sourceId,
    anchor: { clipId: 'clip-1', offset: 0 },
    sourceStart: 0,
    duration: 3,
    volume: 1,
    muted: false,
  };
}

/** A muted film under a ducking bed, and `voices` on a lane of their own, never listened to. */
function film(voices: string[] = ['v'], ducking = true): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 8, size: { width: 8, height: 6 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      ...voices.map((name) => ({
        id: `src-${name}`,
        assetId: name,
        kind: 'audio' as const,
        duration: 3,
        size: { width: 0, height: 0 },
      })),
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: true }],
    overlays: [],
    audio: [
      {
        id: 'lane-1',
        items: [
          {
            ...voice('bed', 'src-m'),
            duration: 8,
            ...(ducking ? { ducking: { amountDb: -12, ramp: 0.5 } } : {}),
          },
        ],
      },
      { id: 'lane-2', items: voices.map((name) => voice(name, `src-${name}`)) },
    ],
  };
}

const speechOf = (current: VideoProject, sourceId: string) =>
  current.sources.find((source) => source.id === sourceId)?.speech;

/** The listener over a project it edits the way the workspace does, one measurement at a time. */
function mountListener(start: VideoProject) {
  return renderHook(() => {
    const [project, setProject] = useState(start);
    const listening = useDuckingListener(project, setProject);
    return { project, setProject, listening };
  });
}

/** Detections that answer only when the test says so, in the order they were asked. */
function heldDetections() {
  const held: { url: string; signal: AbortSignal | undefined; answer: (speech: Speech) => void }[] =
    [];
  hearing.detect.mockImplementation(
    (url, signal) =>
      new Promise((resolve) => {
        held.push({ url, signal, answer: resolve });
      }),
  );
  return held;
}

beforeEach(() => {
  hearing.detect.mockReset();
  hearing.detect.mockResolvedValue([[1, 2]]);
});

describe('useDuckingListener', () => {
  it('listens by itself to a sound a ducking bed goes under, and writes what it heard', async () => {
    const held = heldDetections();
    const { result } = mountListener(film());
    await waitFor(() => {
      expect(held).toHaveLength(1);
    });
    expect(held[0]?.url).toBe('/api/assets/v/download');
    expect(result.current.listening.listening).toBe(true);
    expect(result.current.listening.refusal).toBe('videoStudio.audio.exportListening');
    await act(async () => {
      held[0]?.answer([[1, 2]]);
      await Promise.resolve();
    });
    expect(speechOf(result.current.project, 'src-v')).toEqual([[1, 2]]);
    expect(result.current.listening.listening).toBe(false);
    expect(result.current.listening.refusal).toBeUndefined();
  });

  it('listens to nothing while no sound ducks, or once everything has been heard', async () => {
    const { result } = mountListener(film(['v'], false));
    const heard = mountListener({
      ...film(),
      sources: film().sources.map((source) => ({ ...source, speech: [] })),
    });
    await act(() => Promise.resolve());
    expect(hearing.detect).not.toHaveBeenCalled();
    expect(result.current.listening.listening).toBe(false);
    expect(heard.result.current.listening.refusal).toBeUndefined();
  });

  it('listens to one sound at a time', async () => {
    const held = heldDetections();
    const { result } = mountListener(film(['v', 'w']));
    await waitFor(() => {
      expect(held).toHaveLength(1);
    });
    await act(async () => {
      held[0]?.answer([]);
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(held).toHaveLength(2);
    });
    expect(held[1]?.url).toBe('/api/assets/w/download');
    await act(async () => {
      held[1]?.answer([[0, 1]]);
      await Promise.resolve();
    });
    expect(speechOf(result.current.project, 'src-v')).toEqual([]);
    expect(speechOf(result.current.project, 'src-w')).toEqual([[0, 1]]);
  });

  it('stops, and writes nothing, once the sound no longer needs hearing', async () => {
    const held = heldDetections();
    const { result } = mountListener(film());
    await waitFor(() => {
      expect(held).toHaveLength(1);
    });
    const muted = film();
    act(() => {
      result.current.setProject({
        ...muted,
        audio: (muted.audio ?? []).map((lane) => ({
          ...lane,
          items: lane.items.map((item) => (item.id === 'v' ? { ...item, muted: true } : item)),
        })),
      });
    });
    expect(held[0]?.signal?.aborted).toBe(true);
    await act(async () => {
      held[0]?.answer([[1, 2]]);
      await Promise.resolve();
    });
    expect(speechOf(result.current.project, 'src-v')).toBeUndefined();
    expect(result.current.listening.listening).toBe(false);
  });

  it('stops when the editor goes', async () => {
    const held = heldDetections();
    const { unmount } = mountListener(film());
    await waitFor(() => {
      expect(held).toHaveLength(1);
    });
    unmount();
    expect(held[0]?.signal?.aborted).toBe(true);
  });

  it('says why a sound could not be heard and does not listen again until asked', async () => {
    hearing.detect.mockRejectedValueOnce(new Error('Unable to decode audio data'));
    const { result, rerender } = mountListener(film());
    await waitFor(() => {
      expect(result.current.listening.failure).toBe('Unable to decode audio data');
    });
    expect(result.current.listening.listening).toBe(false);
    expect(result.current.listening.refusal).toBe(
      'videoStudio.audio.exportUnheard Unable to decode audio data',
    );
    rerender();
    await act(() => Promise.resolve());
    expect(hearing.detect).toHaveBeenCalledOnce();
    act(() => {
      result.current.listening.retry();
    });
    await waitFor(() => {
      expect(speechOf(result.current.project, 'src-v')).toEqual([[1, 2]]);
    });
    expect(hearing.detect).toHaveBeenCalledTimes(2);
    expect(result.current.listening.failure).toBeUndefined();
  });

  it('writes again, without listening again, a sound it heard that comes back unheard', async () => {
    // What a redo does: the step that brought the sound brings it back as it was recorded.
    const { result } = mountListener(film());
    await waitFor(() => {
      expect(speechOf(result.current.project, 'src-v')).toEqual([[1, 2]]);
    });
    act(() => {
      result.current.setProject(film());
    });
    await waitFor(() => {
      expect(speechOf(result.current.project, 'src-v')).toEqual([[1, 2]]);
    });
    expect(hearing.detect).toHaveBeenCalledOnce();
  });
});

describe('DuckingStatus', () => {
  const shown = (listening: Partial<DuckingListening>) =>
    render(
      <DuckingStatus
        listening={{
          listening: false,
          failure: undefined,
          refusal: undefined,
          retry: vi.fn(),
          ...listening,
        }}
      />,
    );

  it('says ducking is listening, wherever the operator is', () => {
    shown({ listening: true });
    expect(screen.getByRole('status').textContent).toBe('videoStudio.audio.listening');
  });

  it('says why it could not hear a sound', () => {
    shown({ failure: 'Unable to decode audio data' });
    expect(screen.getByRole('alert').textContent).toBe(
      'videoStudio.audio.listenFailed Unable to decode audio data',
    );
  });

  it('says nothing while it has nothing to listen to', () => {
    const { container } = shown({});
    expect(container.textContent).toBe('');
  });
});

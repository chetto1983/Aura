import { act, renderHook, waitFor } from '@testing-library/react';
import { useEffect, useRef } from 'react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useArtifactsPanel } from './useArtifactsPanel';
import { useSurfaceRestore } from './useSurfaceRestore';
import { useWorkerPane } from './useWorkerPane';

const BASE_PANEL_IDS = ['chat-navigation', 'chat-workspace'] as const;

beforeEach(() => {
  localStorage.clear();
  window.matchMedia = (query: string) =>
    ({
      matches: true,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as MediaQueryList;
});

afterEach(() => {
  localStorage.clear();
});

interface StoredWorkerPane {
  readonly conversationId: string;
  readonly childId: string;
  readonly open: boolean;
}

function readStoredWorkerPane(): StoredWorkerPane | null {
  const raw = localStorage.getItem('aura.shell.worker-pane');
  return raw === null ? null : (JSON.parse(raw) as StoredWorkerPane);
}

function useExclusiveRightRail(conversationId = 'thread-1') {
  const surfaces = useSurfaceRestore();
  const closeWorkerRef = useRef<() => void>(() => undefined);
  const artifacts = useArtifactsPanel(surfaces, BASE_PANEL_IDS, () => {
    closeWorkerRef.current();
  });
  const worker = useWorkerPane(surfaces, artifacts.panelIds, artifacts.closePanel, conversationId);

  useEffect(() => {
    closeWorkerRef.current = worker.closeWorker;
  }, [worker.closeWorker]);

  return { artifacts, worker };
}

describe('useWorkerPane', () => {
  it('keeps the worker and Artifacts surfaces mutually exclusive', () => {
    const { result } = renderHook(() => useExclusiveRightRail());

    act(() => {
      result.current.artifacts.openArtifacts();
    });
    expect(result.current.artifacts.artifactsPanelMounted).toBe(true);
    expect(result.current.worker.workerPanelMounted).toBe(false);

    act(() => {
      result.current.worker.openWorker('child-1');
    });
    expect(result.current.artifacts.artifactsPanelMounted).toBe(false);
    expect(result.current.worker.workerPanelMounted).toBe(true);
    expect(result.current.worker.watchedChildId).toBe('child-1');

    act(() => {
      result.current.artifacts.openArtifacts();
    });
    expect(result.current.worker.workerPanelMounted).toBe(false);
    expect(result.current.artifacts.artifactsPanelMounted).toBe(true);
  });

  it('persists the last open worker intent and contributes one dynamic panel id', () => {
    localStorage.setItem(
      'aura.shell.worker-pane',
      JSON.stringify({ conversationId: 'thread-1', childId: 'child-restored', open: true }),
    );

    const { result } = renderHook(() => useExclusiveRightRail());
    expect(result.current.worker.watchedChildId).toBe('child-restored');
    expect(result.current.worker.workerPanelMounted).toBe(true);
    expect(result.current.worker.panelIds).toEqual([...BASE_PANEL_IDS, 'chat-worker']);
  });

  it('clears the watched child identity when the pane closes', async () => {
    const { result } = renderHook(() => useExclusiveRightRail());

    act(() => {
      result.current.worker.openWorker('child-1');
    });
    expect(readStoredWorkerPane()).toEqual({
      conversationId: 'thread-1',
      childId: 'child-1',
      open: true,
    });

    act(() => {
      result.current.worker.closeWorker();
    });

    expect(result.current.worker.watchedChildId).toBe('');
    expect(result.current.worker.workerPanelMounted).toBe(false);
    await waitFor(() => {
      expect(readStoredWorkerPane()).toEqual({ conversationId: '', childId: '', open: false });
    });
  });

  it('does not restore a persisted worker under another conversation', async () => {
    localStorage.setItem(
      'aura.shell.worker-pane',
      JSON.stringify({ conversationId: 'thread-a', childId: 'child-a', open: true }),
    );

    const { result } = renderHook(() => useExclusiveRightRail('thread-b'));

    expect(result.current.worker.watchedChildId).toBe('');
    expect(result.current.worker.workerPanelMounted).toBe(false);
    await waitFor(() => {
      expect(readStoredWorkerPane()).toEqual({ conversationId: '', childId: '', open: false });
    });
  });

  it('closes the persisted worker immediately when the active conversation changes', async () => {
    const { result, rerender } = renderHook(
      ({ conversationId }) => useExclusiveRightRail(conversationId),
      { initialProps: { conversationId: 'thread-a' } },
    );

    act(() => {
      result.current.worker.openWorker('child-a');
    });
    expect(result.current.worker.workerPanelMounted).toBe(true);

    rerender({ conversationId: 'thread-b' });

    expect(result.current.worker.watchedChildId).toBe('');
    expect(result.current.worker.workerPanelMounted).toBe(false);
    await waitFor(() => {
      expect(readStoredWorkerPane()).toEqual({ conversationId: '', childId: '', open: false });
    });
  });
});

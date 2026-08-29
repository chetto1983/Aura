import { act, renderHook } from '@testing-library/react';
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

function useExclusiveRightRail() {
  const surfaces = useSurfaceRestore();
  const closeWorkerRef = useRef<() => void>(() => undefined);
  const artifacts = useArtifactsPanel(surfaces, BASE_PANEL_IDS, () => {
    closeWorkerRef.current();
  });
  const worker = useWorkerPane(surfaces, artifacts.panelIds, artifacts.closePanel);

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
    localStorage.setItem('aura.shell.worker-child', 'child-restored');
    localStorage.setItem('aura.shell.worker-open', '1');

    const { result } = renderHook(() => useExclusiveRightRail());
    expect(result.current.worker.watchedChildId).toBe('child-restored');
    expect(result.current.worker.workerPanelMounted).toBe(true);
    expect(result.current.worker.panelIds).toEqual([...BASE_PANEL_IDS, 'chat-worker']);
  });
});

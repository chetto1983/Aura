import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement, type ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Asset } from '../attachments/types';
import { listThreadAssets } from '../attachments/api';
import { selectAgentArtifacts, useThreadArtifacts } from './useThreadArtifacts';

// useThreadArtifacts (D-10/WEBART-07): the derived agent-only, newest-first view
// over the ASC server list. The pure `selectAgentArtifacts` projection is asserted
// directly (filter + DESC sort), then the hook is exercised with a mocked
// `listThreadAssets` returning a mixed ASC list to prove the wired query yields
// agent-only, deleted-dropped, newest-first rows — and stays idle for an empty id.

// vi.mock is hoisted above the imports, so the `listThreadAssets` binding above is
// already the mock when the module graph loads.
vi.mock('../attachments/api', () => ({
  listThreadAssets: vi.fn(),
}));

const mockList = vi.mocked(listThreadAssets);

function asset(over: Partial<Asset>): Asset {
  return {
    id: 'a',
    status: 'accepted',
    modality: 'document',
    file_name: 'f.txt',
    mime_type: 'text/plain',
    declared_size_bytes: 0,
    size_bytes: 0,
    source_kind: 'agent',
    ...over,
  };
}

// A server list in created_at ASC (oldest first) mixing an upload, two agent
// deliverables, a deleted agent row, and a canceled agent row.
const ASC_SERVER_LIST: readonly Asset[] = [
  asset({ id: 'upload', source_kind: 'web', created_at: '2026-01-01T00:00:00Z' }),
  asset({ id: 'agent-old', source_kind: 'agent', created_at: '2026-01-02T00:00:00Z' }),
  asset({
    id: 'agent-deleted',
    source_kind: 'agent',
    status: 'deleted',
    created_at: '2026-01-03T00:00:00Z',
  }),
  asset({
    id: 'agent-canceled',
    source_kind: 'agent',
    status: 'canceled',
    created_at: '2026-01-04T00:00:00Z',
  }),
  asset({ id: 'agent-new', source_kind: 'agent', created_at: '2026-01-05T00:00:00Z' }),
];

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return createElement(QueryClientProvider, { client }, children);
  };
}

describe('selectAgentArtifacts (pure projection)', () => {
  it('keeps agent-only rows newest-first and drops uploads/deleted/canceled', () => {
    const out = selectAgentArtifacts(ASC_SERVER_LIST);
    expect(out.map((a) => a.id)).toEqual(['agent-new', 'agent-old']);
  });

  it('does not mutate the input array order (sorts a copy)', () => {
    const input = [...ASC_SERVER_LIST];
    selectAgentArtifacts(input);
    expect(input.map((a) => a.id)).toEqual(ASC_SERVER_LIST.map((a) => a.id));
  });

  it('tolerates missing created_at without throwing', () => {
    const out = selectAgentArtifacts([
      asset({ id: 'no-date' }), // no created_at → sorts after a dated row
      asset({ id: 'dated', created_at: '2026-02-01T00:00:00Z' }),
    ]);
    expect(out.map((a) => a.id)).toEqual(['dated', 'no-date']);
  });
});

describe('useThreadArtifacts (wired query)', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('reuses listThreadAssets and returns agent-only, newest-first rows', async () => {
    mockList.mockResolvedValue([...ASC_SERVER_LIST]);
    const { result } = renderHook(() => useThreadArtifacts('t-1'), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.data).toBeDefined();
    });
    expect(mockList).toHaveBeenCalledWith('t-1', expect.anything());
    expect(result.current.data?.map((a) => a.id)).toEqual(['agent-new', 'agent-old']);
  });

  it('is disabled (idle, no fetch) for an empty threadId', () => {
    const { result } = renderHook(() => useThreadArtifacts(''), { wrapper: wrapper() });
    expect(result.current.fetchStatus).toBe('idle');
    expect(mockList).not.toHaveBeenCalled();
  });
});

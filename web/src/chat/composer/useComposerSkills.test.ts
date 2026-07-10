import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { fetchComposerSkills, type ComposerSkillRow } from './api';
import { useComposerSkills } from './useComposerSkills';

vi.mock('./api', () => ({
  fetchComposerSkills: vi.fn(),
}));

const mockFetch = vi.mocked(fetchComposerSkills);

const ROWS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
];

describe('useComposerSkills', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('fetches once on mount and returns the rows', async () => {
    mockFetch.mockResolvedValue(ROWS);
    const { result } = renderHook(() => useComposerSkills());

    await waitFor(() => {
      expect(result.current).toEqual(ROWS);
    });
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('degrades to [] when the fetch rejects (D-09), never throwing to the caller', async () => {
    mockFetch.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useComposerSkills());

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
    expect(result.current).toEqual([]);
  });
});

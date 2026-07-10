import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { usePinnedSkill } from './usePinnedSkill';
import type { ComposerSkillRow } from './api';

const ROW: ComposerSkillRow = {
  name: 'skill-creator',
  description: 'Create a new skill',
  type: 'instruction',
};

describe('usePinnedSkill', () => {
  it('starts with no pinned skill', () => {
    const { result } = renderHook(() => usePinnedSkill());
    expect(result.current.pinnedSkill).toBeNull();
  });

  it('pins a row and clears it back to null', () => {
    const { result } = renderHook(() => usePinnedSkill());

    act(() => {
      result.current.setPinnedSkill(ROW);
    });
    expect(result.current.pinnedSkill).toEqual(ROW);

    act(() => {
      result.current.setPinnedSkill(null);
    });
    expect(result.current.pinnedSkill).toBeNull();
  });
});

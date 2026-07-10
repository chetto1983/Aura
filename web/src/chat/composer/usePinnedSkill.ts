import { useState } from 'react';
import type { ComposerSkillRow } from './api';

export interface PinnedSkillSeam {
  readonly pinnedSkill: ComposerSkillRow | null;
  readonly setPinnedSkill: (row: ComposerSkillRow | null) => void;
}

// usePinnedSkill mirrors the useAttachmentUploads lifted-seam pattern: it owns the pinnedSkill
// state internally and returns the seam so ExternalStoreChat destructures it instead of an
// inline useState — which keeps ExternalStoreChat.tsx under the 600-LOC cap.
export function usePinnedSkill(): PinnedSkillSeam {
  const [pinnedSkill, setPinnedSkill] = useState<ComposerSkillRow | null>(null);
  return { pinnedSkill, setPinnedSkill };
}

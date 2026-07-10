import { useEffect, useState } from 'react';
import { fetchComposerSkills, type ComposerSkillRow } from './api';

// useComposerSkills fetches the active skills once on mount for the composer '/' picker.
// fetchComposerSkills already degrades to [] on any throw (the D-09 no-op source); the extra
// catch + mounted guard keep a late rejection or an unmount from stranding the caller.
export function useComposerSkills(): readonly ComposerSkillRow[] {
  const [skills, setSkills] = useState<readonly ComposerSkillRow[]>([]);
  useEffect(() => {
    let active = true;
    void fetchComposerSkills()
      .then((rows) => {
        if (active) setSkills(rows);
      })
      .catch(() => {
        if (active) setSkills([]);
      });
    return () => {
      active = false;
    };
  }, []);
  return skills;
}

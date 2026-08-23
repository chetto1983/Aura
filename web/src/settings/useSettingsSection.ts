import { useCallback, useState } from 'react';
import {
  isSettingsSectionId,
  type SettingsSectionDef,
  type SettingsSectionId,
} from './settingsSections';

const SETTINGS_SECTION_KEY = 'aura.settings.section';
const DEFAULT_SECTION: SettingsSectionId = 'profile';

function readSection(): SettingsSectionId {
  try {
    const value = localStorage.getItem(SETTINGS_SECTION_KEY);
    return isSettingsSectionId(value) ? value : DEFAULT_SECTION;
  } catch {
    return DEFAULT_SECTION;
  }
}

/**
 * Persist the pane a later mount should open on. Exported for the `?settings=<id>` deep link,
 * which is handled in AppShell (it also has to flip the surface) before this lazy module loads.
 */
export function storeSettingsSection(section: SettingsSectionId) {
  try {
    localStorage.setItem(SETTINGS_SECTION_KEY, section);
  } catch {
    // Storage is optional; the visible state still updates.
  }
}

export function useSettingsSection(available: readonly SettingsSectionDef[]) {
  const [section, setSectionState] = useState<SettingsSectionId>(readSection);

  const setSection = useCallback((next: SettingsSectionId) => {
    setSectionState(next);
    storeSettingsSection(next);
  }, []);

  // Resolved rather than corrected in an effect: a stored admin-only pane restored into a
  // non-admin session falls back to the first visible one on the SAME render, so the rail
  // never paints a selection the identity cannot open.
  const active = available.some((candidate) => candidate.id === section)
    ? section
    : (available[0]?.id ?? DEFAULT_SECTION);

  return { section: active, setSection };
}

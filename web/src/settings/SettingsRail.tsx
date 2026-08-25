import { useTranslation } from 'react-i18next';
import {
  SETTINGS_GROUPS,
  type SettingsSectionDef,
  type SettingsSectionId,
} from './settingsSections';
import { SectionRail, type SectionRailGroup } from '@/components/SectionRail';

interface SettingsRailProps {
  readonly sections: readonly SettingsSectionDef[];
  readonly activeId: SettingsSectionId;
  readonly onSelect: (id: SettingsSectionId) => void;
}

// The Settings half of the shared SectionRail: it owns the grouping and the i18n keys, the
// rail owns the two layouts (column from `lg`, scrollable strip below it). `onSelect` narrows
// the rail's plain string back to a SettingsSectionId — the ids it hands back can only be the
// ones this component put in.
export function SettingsRail({ sections, activeId, onSelect }: SettingsRailProps) {
  const { t } = useTranslation();
  const groups: SectionRailGroup[] = SETTINGS_GROUPS.map((group) => ({
    id: group,
    caption: t(`settings.groups.${group}`),
    items: sections
      .filter((section) => section.group === group)
      .map((section) => ({ id: section.id, icon: section.icon, label: t(section.labelKey) })),
  }));

  return (
    <SectionRail
      id="settings"
      label={t('settings.rail.label')}
      groups={groups}
      activeId={activeId}
      onSelect={(id) => {
        onSelect(id as SettingsSectionId);
      }}
    />
  );
}

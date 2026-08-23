import { Gauge, Link2, Send, Server, UserRound, Users, Waypoints } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

// The Settings rail (Material's "15+ settings belong on their own screens"): seven panes in
// three groups instead of one scroll with nine headings. `labelKey` is deliberately the
// PANE'S OWN heading key, not a rail-only string — the entry that opens a section and the
// section's title are then the same string by construction and cannot drift apart.
export type SettingsSectionId =
  | 'profile'
  | 'model'
  | 'budget'
  | 'backends'
  | 'identities'
  | 'channels'
  | 'sharing';

export type SettingsGroupId = 'personal' | 'runtime' | 'access';

export interface SettingsSectionDef {
  readonly id: SettingsSectionId;
  readonly group: SettingsGroupId;
  readonly icon: LucideIcon;
  readonly labelKey: string;
  /** Writes behind governance.write; hidden from a non-admin rail (server gates the routes). */
  readonly adminOnly: boolean;
}

export const SETTINGS_GROUPS: readonly SettingsGroupId[] = ['personal', 'runtime', 'access'];

export const SETTINGS_SECTIONS: readonly SettingsSectionDef[] = [
  {
    id: 'profile',
    group: 'personal',
    icon: UserRound,
    labelKey: 'profile.heading',
    adminOnly: false,
  },
  {
    id: 'model',
    group: 'runtime',
    icon: Waypoints,
    labelKey: 'settings.modelRouting.heading',
    adminOnly: true,
  },
  {
    id: 'budget',
    group: 'runtime',
    icon: Gauge,
    labelKey: 'settings.tokens.heading',
    adminOnly: true,
  },
  {
    id: 'backends',
    group: 'runtime',
    icon: Server,
    labelKey: 'settings.backends.heading',
    adminOnly: true,
  },
  {
    id: 'identities',
    group: 'access',
    icon: Users,
    labelKey: 'settings.identity.heading',
    adminOnly: true,
  },
  {
    id: 'channels',
    group: 'access',
    icon: Send,
    labelKey: 'settings.telegram.heading',
    adminOnly: true,
  },
  {
    id: 'sharing',
    group: 'access',
    icon: Link2,
    labelKey: 'share.settings.heading',
    adminOnly: true,
  },
];

export function visibleSections(isAdmin: boolean): readonly SettingsSectionDef[] {
  return isAdmin ? SETTINGS_SECTIONS : SETTINGS_SECTIONS.filter((section) => !section.adminOnly);
}

export function isSettingsSectionId(value: string | null): value is SettingsSectionId {
  return SETTINGS_SECTIONS.some((section) => section.id === value);
}

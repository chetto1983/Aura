import { Cloud, Gauge, Link2, Mail, Send, Server, UserRound, Users, Waypoints } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

// The Settings rail (Material's "15+ settings belong on their own screens"): nine panes in
// three groups instead of one scroll, of which a member sees only the personal group.
// `labelKey` is deliberately the PANE'S OWN heading key, not a rail-only string — the entry that
// opens a section and the section's title are then the same string by construction and cannot
// drift apart.
export type SettingsSectionId =
  | 'profile'
  | 'mail'
  | 'model'
  | 'budget'
  | 'backends'
  | 'identities'
  | 'channels'
  | 'sharing'
  | 'remote-access';

export type SettingsGroupId = 'personal' | 'runtime' | 'access';

export interface SettingsSectionDef {
  readonly id: SettingsSectionId;
  readonly group: SettingsGroupId;
  readonly icon: LucideIcon;
  readonly labelKey: string;
  /** An administrator's pane; hidden from a member's rail (the server gates the routes). */
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
    id: 'mail',
    group: 'personal',
    icon: Mail,
    labelKey: 'settings.mail.heading',
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
  {
    id: 'remote-access',
    group: 'access',
    icon: Cloud,
    labelKey: 'remoteAccess.heading',
    adminOnly: true,
  },
];

export function visibleSections(isAdmin: boolean): readonly SettingsSectionDef[] {
  return isAdmin ? SETTINGS_SECTIONS : SETTINGS_SECTIONS.filter((section) => !section.adminOnly);
}

export function isSettingsSectionId(value: string | null): value is SettingsSectionId {
  return SETTINGS_SECTIONS.some((section) => section.id === value);
}

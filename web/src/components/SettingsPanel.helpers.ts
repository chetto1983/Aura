import type { SettingItem } from '@/types/api';

/** Pure helper exported for unit testing.
 * Extracted from SettingsPanel.tsx for `react-refresh/only-export-components`
 * compliance — fast refresh requires component files to export only components.
 */
export function computeSourceBadge(
  item: Pick<SettingItem, 'read_only' | 'restart_required' | 'source' | 'is_secret' | 'active_value'>,
  dirty: boolean,
  t: (key: string) => string,
) {
  if (item.read_only) return { label: t('settings.badge.readOnly'), cls: 'bg-zinc-500/12 text-zinc-700 dark:text-zinc-300 border-zinc-500/40' };
  if (dirty) return { label: t('settings.badge.edited'), cls: 'bg-amber-500/12 text-amber-700 dark:text-amber-300 border-amber-500/40' };
  if (item.restart_required) return { label: t('settings.badge.restart'), cls: 'bg-orange-500/12 text-orange-700 dark:text-orange-300 border-orange-500/40' };
  switch (item.source) {
    case 'db':
      if (item.is_secret && !item.active_value) return { label: t('settings.badge.unset'), cls: 'bg-muted/50 text-foreground border-border' };
      return { label: t('settings.badge.saved'), cls: 'bg-primary/12 text-cyan-700 dark:text-cyan-300 border-primary/40' };
    case 'env':
      return { label: t('settings.badge.env'), cls: 'bg-sky-500/12 text-sky-700 dark:text-sky-300 border-sky-500/40' };
    default:
      return { label: t('settings.badge.unset'), cls: 'bg-muted/50 text-foreground border-border' };
  }
}

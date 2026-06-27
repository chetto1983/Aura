import { useTranslation } from 'react-i18next';
import { LIVE_MODES, type SurfaceIntent } from './modes';
import { Button } from '@/components/ui/button';

export function ModeTabBar({
  active,
  onSelect,
}: {
  readonly active: SurfaceIntent;
  readonly onSelect: (mode: SurfaceIntent) => void;
}) {
  const { t } = useTranslation();
  return (
    <nav
      aria-label={t('shell.mobileModes')}
      className="shell-mode-tabbar flex min-w-0 overflow-x-auto overscroll-x-contain border-t border-border bg-surface md:hidden"
    >
      {LIVE_MODES.map((mode) => {
        return (
          <Button
            key={mode}
            type="button"
            variant="ghost"
            aria-current={mode === active ? 'page' : undefined}
            onClick={() => {
              onSelect(mode);
            }}
            className="min-w-[6.5rem] flex-1 shrink-0 rounded-none px-1 text-[0.75rem] font-medium text-text-muted aria-[current=page]:bg-surface-3 aria-[current=page]:text-accent-text focus-visible:outline-2 focus-visible:outline-inset focus-visible:outline-accent"
          >
            {t(`shell.modes.${mode}`)}
          </Button>
        );
      })}
    </nav>
  );
}

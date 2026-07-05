import { useTranslation } from 'react-i18next';
import { MODES, isLiveSurfaceIntent, type SurfaceIntent } from './modes';
import { Button } from '@/components/ui/button';

export function ModeSwitcher({
  active,
  onSelect,
  modes = MODES,
}: {
  readonly active: SurfaceIntent;
  readonly onSelect: (mode: SurfaceIntent) => void;
  // The visible mode list (MUSR-01 capability-gated by AppShell); defaults to the full set so
  // callers that do not gate (and the shell unit tests) keep the complete switcher.
  readonly modes?: readonly SurfaceIntent[] | undefined;
}) {
  const { t } = useTranslation();
  return (
    <nav
      aria-label={t('shell.primaryNav')}
      className="shell-mode-switcher hidden min-w-0 max-w-full items-center gap-1 overflow-x-auto overscroll-x-contain pb-1 text-text-muted md:flex"
    >
      {modes.map((mode) => {
        const disabled = !isLiveSurfaceIntent(mode);
        return (
          <Button
            key={mode}
            type="button"
            variant="ghost"
            size="sm"
            aria-current={mode === active ? 'page' : undefined}
            aria-disabled={disabled ? true : undefined}
            title={disabled ? t('shell.modeUnavailable') : undefined}
            onClick={() => {
              if (!disabled) onSelect(mode);
            }}
            className={`shrink-0 px-2.5 text-xs font-medium aria-[current=page]:bg-surface-3 aria-[current=page]:text-text ${
              disabled
                ? 'cursor-not-allowed text-text-disabled'
                : 'hover:bg-surface-2 hover:text-text'
            }`}
          >
            {t(`shell.modes.${mode}`)}
          </Button>
        );
      })}
    </nav>
  );
}

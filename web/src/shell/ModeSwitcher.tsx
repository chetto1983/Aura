import { useTranslation } from 'react-i18next';
import { MODES, isLiveSurfaceIntent, type SurfaceIntent } from './modes';

export function ModeSwitcher({
  active,
  onSelect,
}: {
  readonly active: SurfaceIntent;
  readonly onSelect: (mode: SurfaceIntent) => void;
}) {
  const { t } = useTranslation();
  return (
    <nav
      aria-label={t('shell.primaryNav')}
      className="shell-mode-switcher hidden min-w-0 max-w-full items-center gap-1 overflow-x-auto overscroll-x-contain pb-1 text-text-muted md:flex"
    >
      {MODES.map((mode) => {
        const disabled = !isLiveSurfaceIntent(mode);
        return (
          <button
            key={mode}
            type="button"
            aria-current={mode === active ? 'page' : undefined}
            aria-disabled={disabled ? true : undefined}
            title={disabled ? t('shell.modeUnavailable') : undefined}
            onClick={() => {
              if (!disabled) onSelect(mode);
            }}
            className={`min-h-8 shrink-0 rounded-[var(--radius-md)] px-2.5 text-xs font-medium outline-none transition aria-[current=page]:bg-surface-3 aria-[current=page]:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${
              disabled
                ? 'cursor-not-allowed text-text-disabled'
                : 'hover:bg-surface-2 hover:text-text'
            }`}
          >
            {t(`shell.modes.${mode}`)}
          </button>
        );
      })}
    </nav>
  );
}

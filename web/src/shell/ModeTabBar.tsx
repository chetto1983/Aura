import { useTranslation } from 'react-i18next';
import { MODES, isLiveSurfaceIntent, type SurfaceIntent } from './modes';

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
      className="grid grid-cols-5 border-t border-border bg-surface md:hidden"
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
            className={`min-h-11 px-1 text-[0.75rem] font-medium outline-none aria-[current=page]:bg-surface-3 aria-[current=page]:text-accent-text focus-visible:outline-2 focus-visible:outline-inset focus-visible:outline-accent ${
              disabled ? 'cursor-not-allowed text-text-disabled' : 'text-text-muted'
            }`}
          >
            {t(`shell.modes.${mode}`)}
          </button>
        );
      })}
    </nav>
  );
}

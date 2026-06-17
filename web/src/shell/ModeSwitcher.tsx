import { useTranslation } from 'react-i18next';
import { MODES, type SurfaceIntent } from './modes';

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
      className="hidden items-center gap-1 text-text-muted md:flex"
    >
      {MODES.map((mode) => (
        <button
          key={mode}
          type="button"
          aria-current={mode === active ? 'page' : undefined}
          onClick={() => {
            onSelect(mode);
          }}
          className="min-h-8 rounded-[var(--radius-md)] px-2.5 text-xs font-medium outline-none transition aria-[current=page]:bg-surface-3 aria-[current=page]:text-text hover:bg-surface-2 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t(`shell.modes.${mode}`)}
        </button>
      ))}
    </nav>
  );
}

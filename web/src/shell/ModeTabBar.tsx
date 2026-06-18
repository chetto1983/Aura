import { useTranslation } from 'react-i18next';
import { MODES, type SurfaceIntent } from './modes';

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
      {MODES.map((mode) => (
        <button
          key={mode}
          type="button"
          aria-current={mode === active ? 'page' : undefined}
          onClick={() => {
            onSelect(mode);
          }}
          className="min-h-11 px-1 text-[0.75rem] font-medium text-text-muted outline-none aria-[current=page]:bg-surface-3 aria-[current=page]:text-accent-text focus-visible:outline-2 focus-visible:outline-inset focus-visible:outline-accent"
        >
          {t(`shell.modes.${mode}`)}
        </button>
      ))}
    </nav>
  );
}

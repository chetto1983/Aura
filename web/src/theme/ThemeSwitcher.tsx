import { useState } from 'react';
import { Moon, Sun } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getTheme, setTheme, THEMES, type Theme } from './applyTheme';
import { Button } from '@/components/ui/button';

const LABEL_KEYS: Record<Theme, string> = {
  light: 'theme.light',
  dark: 'theme.dark',
};

export function ThemeSwitcher({ className = '' }: { readonly className?: string }) {
  const { t } = useTranslation();
  const [active, setActive] = useState<Theme>(getTheme);

  function choose(theme: Theme) {
    setTheme(theme);
    setActive(theme);
  }

  return (
    <div
      role="group"
      aria-label={t('theme.switcherLabel')}
      className={`flex shrink-0 rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5 ${className}`}
    >
      {THEMES.map((theme) => (
        <Button
          key={theme}
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t(LABEL_KEYS[theme])}
          aria-pressed={active === theme}
          title={t(LABEL_KEYS[theme])}
          onClick={() => {
            choose(theme);
          }}
          className="h-[32px] min-h-[32px] w-[32px] rounded-[calc(var(--radius-md)-2px)] text-text-muted aria-pressed:bg-accent aria-pressed:text-on-accent hover:text-text"
        >
          {theme === 'light' ? (
            <Sun data-icon="icon" aria-hidden="true" focusable="false" />
          ) : (
            <Moon data-icon="icon" aria-hidden="true" focusable="false" />
          )}
        </Button>
      ))}
    </div>
  );
}

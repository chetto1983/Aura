import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getTheme, setTheme, THEMES, type Theme } from './applyTheme';

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
        <button
          key={theme}
          type="button"
          aria-label={t(LABEL_KEYS[theme])}
          aria-pressed={active === theme}
          title={t(LABEL_KEYS[theme])}
          onClick={() => {
            choose(theme);
          }}
          className="flex min-h-7 min-w-8 items-center justify-center rounded-[calc(var(--radius-md)-2px)] text-text-muted outline-none transition aria-pressed:bg-accent aria-pressed:text-on-accent hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {theme === 'light' ? <SunIcon /> : <MoonIcon />}
        </button>
      ))}
    </div>
  );
}

function SunIcon() {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2" />
      <path d="M12 20v2" />
      <path d="m4.93 4.93 1.41 1.41" />
      <path d="m17.66 17.66 1.41 1.41" />
      <path d="M2 12h2" />
      <path d="M20 12h2" />
      <path d="m6.34 17.66-1.41 1.41" />
      <path d="m19.07 4.93-1.41 1.41" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20.99 12.7A8.5 8.5 0 1 1 11.3 3.01a6.5 6.5 0 0 0 9.69 9.69Z" />
    </svg>
  );
}

import { useTranslation } from 'react-i18next';
import { changeAppLanguage, normalizeLanguage, supportedLanguages } from './i18n';
import type { AppLanguage } from './resources';

const LANGUAGE_LABEL_KEYS: Record<AppLanguage, string> = {
  en: 'language.english',
  it: 'language.italian',
};

export function LanguageSwitcher({ className = '' }: { className?: string }) {
  const { t, i18n } = useTranslation();
  const active = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language) ?? 'en';

  return (
    <div
      role="group"
      aria-label={t('language.switcherLabel')}
      className={`flex shrink-0 rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5 ${className}`}
    >
      {supportedLanguages.map((language) => (
        <button
          key={language}
          type="button"
          aria-label={t(LANGUAGE_LABEL_KEYS[language])}
          aria-pressed={active === language}
          onClick={() => {
            void changeAppLanguage(language);
          }}
          className="min-h-7 min-w-8 rounded-[calc(var(--radius-md)-2px)] px-2 text-xs font-medium text-text-muted outline-none transition aria-pressed:bg-accent aria-pressed:text-on-accent hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {language.toUpperCase()}
        </button>
      ))}
    </div>
  );
}

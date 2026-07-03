import { useTranslation } from 'react-i18next';
import { changeAppLanguage, normalizeLanguage, supportedLanguages } from './i18n';
import type { AppLanguage } from './resources';
import { Button } from '@/components/ui/button';

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
        <Button
          key={language}
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t(LANGUAGE_LABEL_KEYS[language])}
          aria-pressed={active === language}
          onClick={() => {
            void changeAppLanguage(language);
          }}
          className="h-[32px] min-h-[32px] min-w-[32px] rounded-[calc(var(--radius-md)-2px)] px-2 text-xs font-medium text-text-muted aria-pressed:bg-accent aria-pressed:text-on-accent hover:text-text"
        >
          {language.toUpperCase()}
        </Button>
      ))}
    </div>
  );
}

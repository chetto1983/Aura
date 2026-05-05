import { useTranslation } from 'react-i18next';
import { SUPPORTED_LANGS, type SupportedLang } from '@/i18n';

export function useLocale() {
  const { t, i18n } = useTranslation();

  const locale = normalizeSupportedLang(i18n.resolvedLanguage || i18n.language);

  const formatDate = (
    date: Date | number | string,
    options?: Intl.DateTimeFormatOptions,
  ): string => {
    const d = typeof date === 'string' ? new Date(date) : date;
    return new Intl.DateTimeFormat(locale, options).format(d);
  };

  const formatRelative = (
    value: number,
    unit: Intl.RelativeTimeFormatUnit,
  ): string => {
    return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(
      value,
      unit,
    );
  };

  const formatNumber = (
    value: number,
    options?: Intl.NumberFormatOptions,
  ): string => {
    return new Intl.NumberFormat(locale, options).format(value);
  };

  return { t, locale, formatDate, formatRelative, formatNumber };
}

function normalizeSupportedLang(value: string): SupportedLang {
  const base = value.toLowerCase().split(/[-_]/)[0] as SupportedLang;
  return SUPPORTED_LANGS.includes(base) ? base : 'en';
}

import { useTranslation } from 'react-i18next';
import type { SupportedLang } from '@/i18n';

export function useLocale() {
  const { t, i18n } = useTranslation();

  const locale = i18n.language as SupportedLang;

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

import { en } from './messages/en.js';
import { it } from './messages/it.js';

export type Locale = 'en' | 'it';
export type MessageKey = keyof typeof en;
export type Translator = (
  key: MessageKey,
  values?: Readonly<Record<string, string | number>>,
) => string;

const catalogs: Record<Locale, Record<MessageKey, string>> = { en, it };

export function detectLocale(locale?: string): Locale {
  return locale?.replace('_', '-').toLowerCase().startsWith('it') ? 'it' : 'en';
}

export function createTranslator(locale: Locale): Translator {
  return (key, values = {}) => catalogs[locale][key].replace(
    /\{([a-zA-Z][a-zA-Z0-9]*)\}/g,
    (token, name: string) => name in values ? String(values[name]) : token,
  );
}

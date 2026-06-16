import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { resources, type AppLanguage } from './resources';

const STORAGE_KEY = 'aura.language';

export const supportedLanguages = ['en', 'it'] as const satisfies readonly AppLanguage[];

export function normalizeLanguage(value: string | null | undefined): AppLanguage | undefined {
  const base = value?.trim().toLowerCase().split('-')[0];
  if (base === 'en' || base === 'it') {
    return base;
  }
  return undefined;
}

function storedLanguage(): AppLanguage | undefined {
  /* v8 ignore next 2 -- SSR guard: window is always defined in the SPA + jsdom */
  if (typeof window === 'undefined') {
    return undefined;
  }
  return normalizeLanguage(window.localStorage.getItem(STORAGE_KEY));
}

function queryLanguage(): AppLanguage | undefined {
  /* v8 ignore next 2 -- SSR guard: window is always defined in the SPA + jsdom */
  if (typeof window === 'undefined') {
    return undefined;
  }
  return normalizeLanguage(new URLSearchParams(window.location.search).get('lang'));
}

function browserLanguage(): AppLanguage | undefined {
  /* v8 ignore next 2 -- SSR guard: navigator is always defined in the SPA + jsdom */
  if (typeof navigator === 'undefined') {
    return undefined;
  }
  const candidates = navigator.languages.length ? navigator.languages : [navigator.language];
  for (const candidate of candidates) {
    const normalized = normalizeLanguage(candidate);
    if (normalized) {
      return normalized;
    }
  }
  /* v8 ignore next -- jsdom always reports an en/it-resolvable navigator language */
  return undefined;
}

function detectInitialLanguage(): AppLanguage {
  return queryLanguage() ?? storedLanguage() ?? browserLanguage() ?? 'en';
}

function persistLanguage(language: string) {
  const normalized = normalizeLanguage(language) ?? 'en';
  if (typeof document !== 'undefined') {
    document.documentElement.lang = normalized;
  }
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(STORAGE_KEY, normalized);
  }
}

void i18n.use(initReactI18next).init({
  resources,
  lng: detectInitialLanguage(),
  fallbackLng: 'en',
  supportedLngs: supportedLanguages,
  defaultNS: 'translation',
  returnNull: false,
  interpolation: {
    escapeValue: false,
  },
  react: {
    useSuspense: false,
  },
});

persistLanguage(i18n.language);
i18n.on('languageChanged', persistLanguage);

export function changeAppLanguage(language: AppLanguage): Promise<unknown> {
  return i18n.changeLanguage(language);
}

export default i18n;

import { useCallback, useEffect, useMemo, useState } from "react";

export type AppTheme = "light" | "dark" | "contrast";

const STORAGE_KEY = "aura-ui-theme";
const THEME_EVENT = "aura-ui-theme-change";
const THEMES: AppTheme[] = ["light", "dark", "contrast"];

function isAppTheme(value: unknown): value is AppTheme {
  return value === "light" || value === "dark" || value === "contrast";
}

function readInitialTheme(): AppTheme {
  if (typeof window === "undefined") return "dark";
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === "light" || saved === "dark" || saved === "contrast") return saved;
  // 10e: dark by default. Honor an explicit OS preference for light, but
  // when the OS is set to dark or has no preference, use Aura's dark theme
  // (the dashboard was designed dark-first).
  if (window.matchMedia?.("(prefers-color-scheme: light)").matches) return "light";
  return "dark";
}

function applyTheme(theme: AppTheme) {
  const root = document.documentElement;
  root.dataset.theme = theme;
  root.classList.toggle("dark", theme === "dark" || theme === "contrast");
}

export function useAppTheme() {
  const [theme, setThemeState] = useState<AppTheme>(readInitialTheme);

  useEffect(() => {
    applyTheme(theme);
    window.localStorage.setItem(STORAGE_KEY, theme);
    window.dispatchEvent(new CustomEvent<AppTheme>(THEME_EVENT, { detail: theme }));
  }, [theme]);

  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const next = (event as CustomEvent<AppTheme>).detail;
      if (isAppTheme(next)) setThemeState(next);
    };
    const handleStorage = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY && isAppTheme(event.newValue)) {
        setThemeState(event.newValue);
      }
    };
    window.addEventListener(THEME_EVENT, handleThemeChange);
    window.addEventListener("storage", handleStorage);
    return () => {
      window.removeEventListener(THEME_EVENT, handleThemeChange);
      window.removeEventListener("storage", handleStorage);
    };
  }, []);

  const setTheme = useCallback((next: AppTheme) => {
    setThemeState(next);
  }, []);

  const cycleTheme = useCallback(() => {
    setThemeState((current) => {
      const index = THEMES.indexOf(current);
      return THEMES[(index + 1) % THEMES.length];
    });
  }, []);

  const label = useMemo(() => {
    if (theme === "dark") return "Tema scuro";
    if (theme === "contrast") return "Tema contrasto";
    return "Tema chiaro";
  }, [theme]);

  return { theme, setTheme, cycleTheme, label };
}

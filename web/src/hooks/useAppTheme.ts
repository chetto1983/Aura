import { useCallback, useEffect, useMemo, useState } from "react";

export type AppTheme = "light" | "dark" | "contrast";

const STORAGE_KEY = "sacchi-ui-theme";
const THEMES: AppTheme[] = ["light", "dark", "contrast"];

function readInitialTheme(): AppTheme {
  if (typeof window === "undefined") return "light";
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === "light" || saved === "dark" || saved === "contrast") return saved;
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function applyTheme(theme: AppTheme) {
  const root = document.documentElement;
  root.dataset.theme = theme;
  root.classList.toggle("dark", theme === "dark" || theme === "contrast");
}

export function useAppTheme() {
  const [theme, setTheme] = useState<AppTheme>(readInitialTheme);

  useEffect(() => {
    applyTheme(theme);
    window.localStorage.setItem(STORAGE_KEY, theme);
  }, [theme]);

  const cycleTheme = useCallback(() => {
    setTheme((current) => {
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

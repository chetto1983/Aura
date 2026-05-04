import { Contrast, Moon, Sun } from "lucide-react";
import type { AppTheme } from "../../hooks/useAppTheme";
import { useLocale } from "@/hooks/useLocale";

export function ThemeToggle({
  theme,
  onSelect,
  compact = false,
}: {
  theme: AppTheme;
  onSelect: (theme: AppTheme) => void;
  compact?: boolean;
}) {
  const { t } = useLocale();
  const options: Array<{ value: AppTheme; label: string; short: string; icon: typeof Sun }> = [
    { value: "light", label: "Light", short: "Light", icon: Sun },
    { value: "dark", label: "Dark", short: "Dark", icon: Moon },
    { value: "contrast", label: t('sidebar.highContrast'), short: t('sidebar.highContrast'), icon: Contrast },
  ];

  return (
    <div className={`sacchi-theme-switcher${compact ? " sacchi-theme-switcher--compact" : ""}`} role="group" aria-label={t('common.themeSelector')}>
      {options.map((option) => {
        const Icon = option.icon;
        const active = option.value === theme;
        return (
          <button
            key={option.value}
            type="button"
            className="sacchi-theme-switcher__option"
            data-active={active ? "true" : undefined}
            aria-pressed={active}
            aria-label={t('common.themeOption', { name: option.short })}
            title={t('common.themeOption', { name: option.short })}
            onClick={() => onSelect(option.value)}
          >
            <Icon size={17} aria-hidden="true" />
            {!compact && <span>{option.label}</span>}
          </button>
        );
      })}
    </div>
  );
}

import { Contrast, Moon, Sun } from "lucide-react";
import type { AppTheme } from "../../hooks/useAppTheme";

export function ThemeToggle({
  theme,
  onSelect,
  compact = false,
}: {
  theme: AppTheme;
  onSelect: (theme: AppTheme) => void;
  compact?: boolean;
}) {
  const options: Array<{ value: AppTheme; label: string; short: string; icon: typeof Sun }> = [
    { value: "light", label: "Light", short: "Light", icon: Sun },
    { value: "dark", label: "Dark", short: "Dark", icon: Moon },
    { value: "contrast", label: "Contrasto", short: "Alto contrasto", icon: Contrast },
  ];

  return (
    <div className={`sacchi-theme-switcher${compact ? " sacchi-theme-switcher--compact" : ""}`} role="group" aria-label="Selettore tema">
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
            aria-label={`Tema ${option.short}`}
            title={`Tema ${option.short}`}
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

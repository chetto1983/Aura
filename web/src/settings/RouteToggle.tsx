import type { LucideIcon } from 'lucide-react';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// RouteToggle — the one control for "which route serves this capability". Model routing and
// the embedding backend both ask that question, and as three loose buttons they read as three
// independent actions rather than one choice with one answer. A segmented track says
// mutually-exclusive by shape, and Radix gives it the radiogroup semantics the shape promises.

export interface RouteOption<T extends string> {
  readonly id: T;
  readonly label: string;
  readonly icon: LucideIcon;
  readonly disabled?: boolean;
}

interface RouteToggleProps<T extends string> {
  readonly label: string;
  readonly value: T;
  readonly options: readonly RouteOption<T>[];
  readonly onChange: (next: T) => void;
}

export function RouteToggle<T extends string>({
  label,
  value,
  options,
  onChange,
}: RouteToggleProps<T>) {
  return (
    <ToggleGroup
      type="single"
      spacing={1}
      value={value}
      aria-label={label}
      // Radix reports '' when the pressed item is clicked again. A capability always has a
      // route, so that reads as "this one" and re-applies the current choice: pressing the
      // active route is how an operator repairs a half-written one — the 2026-07-27 regression
      // (provider left behind while the base URL moved) is pinned by a test that does exactly
      // that on the route already selected.
      onValueChange={(next: string) => {
        onChange((next === '' ? value : next) as T);
      }}
      className="w-fit max-w-full flex-wrap rounded-[var(--radius-md)] border border-border bg-surface-3 p-1"
    >
      {options.map((option) => (
        <ToggleGroupItem
          key={option.id}
          value={option.id}
          disabled={option.disabled === true}
          className="h-9 gap-2 rounded-[var(--radius-sm)] px-3.5 text-[13px] font-medium text-text-muted transition-colors hover:bg-surface-2 hover:text-text data-[state=on]:bg-accent data-[state=on]:text-accent-text"
        >
          <option.icon aria-hidden="true" />
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

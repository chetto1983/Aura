import { Fragment, useRef, useState, type KeyboardEvent } from 'react';
import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';

// QuestionOptions is Question Flow's option list (assistant-ui/tool-ui@49a8702
// question-flow.tsx: OptionItem, SelectionIndicator, the listbox keyboard; © 2025 AgentbaseAI
// Inc., MIT, see THIRD_PARTY_NOTICES.md) as radio rows (single) or checkbox rows (multi). Enter
// on a row that is already chosen submits a single choice, so a keyboard user can answer
// without leaving the list.

export interface QuestionOption {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
}

export interface QuestionOptionsProps {
  readonly labelledBy: string;
  readonly describedBy?: string;
  readonly options: readonly QuestionOption[];
  readonly mode: 'single' | 'multi';
  readonly selected: ReadonlySet<string>;
  readonly disabled?: boolean;
  readonly onToggle: (id: string) => void;
  readonly onSubmit?: () => void;
}

export function QuestionOptions({
  labelledBy,
  describedBy,
  options,
  mode,
  selected,
  disabled = false,
  onToggle,
  onSubmit,
}: QuestionOptionsProps) {
  const rows = useRef<(HTMLButtonElement | null)[]>([]);
  const [active, setActive] = useState(() =>
    Math.max(
      options.findIndex((o) => selected.has(o.id)),
      0,
    ),
  );
  const last = options.length - 1;

  function focusAt(index: number) {
    rows.current[index]?.focus();
    setActive(index);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (disabled || options.length === 0) return;
    const moves: Record<string, number> = {
      ArrowDown: active === last ? 0 : active + 1,
      ArrowUp: active === 0 ? last : active - 1,
      Home: 0,
      End: last,
    };
    const target = moves[event.key];
    if (target !== undefined) {
      event.preventDefault();
      focusAt(target);
      return;
    }
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    const option = options[active];
    if (option === undefined) return;
    if (event.key === 'Enter' && mode === 'single' && selected.has(option.id)) onSubmit?.();
    else onToggle(option.id);
  }

  return (
    <div
      role="listbox"
      aria-labelledby={labelledBy}
      {...(describedBy !== undefined ? { 'aria-describedby': describedBy } : {})}
      aria-multiselectable={mode === 'multi'}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      className="flex flex-col px-1"
    >
      {options.map((option, index) => {
        const isSelected = selected.has(option.id);
        return (
          <Fragment key={option.id}>
            {index > 0 ? <div aria-hidden="true" className="h-px bg-border-strong/40" /> : null}
            <button
              ref={(el) => {
                rows.current[index] = el;
              }}
              type="button"
              role="option"
              aria-selected={isSelected}
              tabIndex={index === active ? 0 : -1}
              disabled={disabled}
              onFocus={() => {
                setActive(index);
              }}
              onClick={() => {
                setActive(index);
                onToggle(option.id);
              }}
              className="group relative flex min-h-[50px] w-full items-start gap-3 py-2.5 text-left text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              <span
                aria-hidden="true"
                className="absolute inset-0 -mx-3 -my-0.5 rounded-xl bg-accent/5 opacity-0 transition-opacity group-hover:opacity-100"
              />
              <span className="relative flex h-6 items-center">
                <SelectionIndicator mode={mode} selected={isSelected} />
              </span>
              <span className="relative flex flex-col">
                <span className="leading-6 text-pretty">{option.label}</span>
                {option.description !== undefined ? (
                  <span className="text-sm font-normal text-pretty text-text-muted">
                    {option.description}
                  </span>
                ) : null}
              </span>
            </button>
          </Fragment>
        );
      })}
    </div>
  );
}

interface SelectionIndicatorProps {
  readonly mode: 'single' | 'multi';
  readonly selected: boolean;
}

function SelectionIndicator({ mode, selected }: SelectionIndicatorProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-4 shrink-0 items-center justify-center border-2',
        'motion-safe:transition-colors motion-safe:duration-200',
        mode === 'single' ? 'rounded-full' : 'rounded',
        selected
          ? 'border-accent bg-accent text-primary-foreground motion-safe:animate-in motion-safe:fade-in motion-safe:zoom-in-75'
          : 'border-text-muted/50',
      )}
    >
      {selected && mode === 'multi' ? <Check className="size-3" strokeWidth={3} /> : null}
      {selected && mode === 'single' ? <span className="size-2 rounded-full bg-current" /> : null}
    </span>
  );
}

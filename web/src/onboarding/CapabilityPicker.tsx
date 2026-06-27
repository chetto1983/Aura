import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Checkbox } from '@/components/ui/checkbox';

// CapabilityPicker (ONBD-01a / D-06) — a checklist over the creator's grants returned by /start,
// which are ALREADY '*'-excluded server-side. The picker offers NO '*' option and renders the hint
// that full-access ('*') can't be granted (T-28-06-03 — the server is authoritative; the client
// never invents a capability). Capability names are backend-supplied → rendered as React-escaped
// mono text (identifier shape), NEVER dangerouslySetInnerHTML (T-28-06-05).
//
// Controlled: the wizard owns the selected set; each checkbox toggles its name in/out. An empty
// options list (a creator holding only '*') shows the "no grantable capabilities" copy.

export interface CapabilityPickerProps {
  readonly options: readonly string[];
  readonly selected: ReadonlySet<string>;
  readonly onToggle: (name: string) => void;
}

export function CapabilityPicker({ options, selected, onToggle }: CapabilityPickerProps) {
  const { t } = useTranslation();
  const labelId = useId();
  const hintId = useId();

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 id={labelId} className="font-display text-[22px] font-semibold text-text">
          {t('onboarding.capabilities.label')}
        </h2>
        <p id={hintId} className="text-[13px] leading-relaxed text-text-muted">
          {t('onboarding.capabilities.hint')}
        </p>
      </div>

      {options.length === 0 ? (
        <p className="text-[15.5px] leading-relaxed text-text-muted">
          {t('onboarding.capabilities.none')}
        </p>
      ) : (
        <ul aria-labelledby={labelId} aria-describedby={hintId} className="flex flex-col gap-2">
          {options.map((name) => {
            const checked = selected.has(name);
            return (
              <li key={name}>
                <label
                  className={`flex min-h-[44px] cursor-pointer items-center gap-3 rounded-md border px-3 py-2 transition-colors focus-within:ring-2 focus-within:ring-ring ${
                    checked
                      ? 'border-accent bg-accent/10 text-text'
                      : 'border-border bg-surface-2 text-text hover:border-border-strong'
                  }`}
                >
                  <Checkbox
                    checked={checked}
                    onCheckedChange={() => {
                      onToggle(name);
                    }}
                    className="size-5"
                  />
                  {/* Backend-supplied capability name — React-escaped, mono (identifier). */}
                  <span className="break-all font-mono text-[15.5px]">{name}</span>
                </label>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

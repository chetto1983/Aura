import { useId, useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { setMcpServerEnv, type McpEnvChip } from './governanceApi';

// McpEnvEditForm (MCPW-02) — the four-state redacted env editor that REPLACES the read-only
// env-keys <section> of McpServerDetail in edit mode. It renders the FOUR env states
// (required / optional / missing / placeholder) as a dot + label (NEVER color-alone), masks a
// stored secret as the redacted ${KEY} placeholder with NO eye-reveal (the value was never
// sent — leaving it untouched PRESERVES the secret server-side), and surfaces a SOFT warning
// card (border-warning bg-warning/10 + the offending keys) when a required var is still
// missing/placeholder at save — the save is still ALLOWED (informational, MCPW-02 / D-F2).
//
// SECURITY (T-29-04-01): no raw secret VALUE ever enters the DOM on any state. A secret row's
// initial value is the redacted ${KEY} placeholder, not the value; the value is sent ONLY
// when the operator types a real replacement (rotate). Submitting the unchanged placeholder
// preserves the stored secret (the backend SetServerEnv four-state merge).

/** One editable env row's derived health state (UI-SPEC §2 four-state table). */
export type EnvState = 'required' | 'optional' | 'missing' | 'placeholder';

const STATE_DOT: Record<EnvState, string> = {
  required: 'bg-text-muted',
  optional: 'bg-text-muted',
  missing: 'bg-danger',
  placeholder: 'bg-warning',
};

const STATE_LABEL: Record<EnvState, string> = {
  required: 'governance.mcp.env.state.required',
  optional: 'governance.mcp.env.state.optional',
  missing: 'governance.mcp.env.state.missing',
  placeholder: 'governance.mcp.env.state.placeholder',
};

/** The redacted placeholder literal shown for a stored secret in edit mode. The value is
 * NEVER in the DOM; this `${KEY}` token is what the backend round-trips to preserve. */
function placeholderToken(key: string): string {
  return `\${${key}}`;
}

/** One row's editable model. `secret` masks the field as the redacted ${KEY} placeholder;
 * `present` distinguishes a stored value from a never-set (missing) required var. */
interface EnvRow {
  readonly key: string;
  readonly secret: boolean;
  readonly required: boolean;
  readonly present: boolean;
  value: string;
}

function deriveState(row: EnvRow): EnvState {
  if (row.required && !row.present) return 'missing';
  if (row.required && row.value === placeholderToken(row.key)) return 'placeholder';
  if (row.required) return 'required';
  return 'optional';
}

export interface McpEnvEditFormProps {
  readonly serverName: string;
  /** The stored env-key chips (key-only; a redacted chip is a secret). */
  readonly envKeys: readonly McpEnvChip[];
  /** The recipe RequiredEnv keys, if any — drives required/missing classification. A custom
   * server passes []; every present key is then `optional`. */
  readonly requiredEnv?: readonly string[];
  /** Called on a successful save with whether any untouched secret was preserved, so the
   * parent can surface the "Unchanged secrets were preserved." affirmation in read mode. */
  readonly onSaved?: (preservedSecret: boolean) => void;
  readonly onClose: () => void;
}

export function McpEnvEditForm({
  serverName,
  envKeys,
  requiredEnv = [],
  onSaved,
  onClose,
}: McpEnvEditFormProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();

  const requiredSet = useMemo(() => new Set(requiredEnv), [requiredEnv]);

  const [rows, setRows] = useState<EnvRow[]>(() => {
    const present = envKeys.map<EnvRow>((chip) => ({
      key: chip.key,
      secret: chip.redacted,
      required: requiredSet.has(chip.key),
      present: true,
      value: chip.redacted ? placeholderToken(chip.key) : '',
    }));
    // A required var with no stored chip is a MISSING row (rendered, contributes to the
    // soft warning) so the operator can fill it in place.
    const missing = requiredEnv
      .filter((key) => !envKeys.some((chip) => chip.key === key))
      .map<EnvRow>((key) => ({ key, secret: true, required: true, present: false, value: '' }));
    return [...present, ...missing];
  });

  const mutation = useMutation({
    mutationFn: (vars: { readonly env: readonly string[]; readonly preserved: boolean }) =>
      setMcpServerEnv(serverName, vars.env).then(() => vars.preserved),
    onSuccess: (preserved: boolean) => {
      void queryClient.invalidateQueries({ queryKey: ['governance', 'mcp'] });
      // The affirmation is surfaced by the parent in read mode (the section returns to read
      // mode on success — UI-SPEC §2).
      onSaved?.(preserved);
      onClose();
    },
  });

  function setRowValue(key: string, value: string) {
    setRows((prev) => prev.map((row) => (row.key === key ? { ...row, value } : row)));
  }

  // The soft-warning offending keys: every required var still missing or holding its
  // placeholder literal. The save stays ALLOWED (informational, not a blocker).
  const offending = rows
    .filter((row) => row.required)
    .filter((row) => !row.present || row.value === '' || row.value === placeholderToken(row.key))
    .map((row) => row.key);

  function submit() {
    // Whether any secret row was left at its redacted placeholder (so the parent can surface
    // the "Unchanged secrets were preserved." affirmation on save).
    const preserved = rows.some(
      (row) => row.secret && row.present && row.value === placeholderToken(row.key),
    );
    // A secret left at its redacted placeholder is submitted verbatim → the backend
    // PRESERVES the stored value (it never overwrites with the placeholder). A missing
    // required var with no typed value is omitted (nothing to write).
    const env = rows
      .filter((row) => row.present || row.value !== '')
      .map((row) => `${row.key}=${row.value}`);
    mutation.mutate({ env, preserved });
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h4
        id={headingId}
        className="text-[13px] font-semibold uppercase tracking-wide text-text-muted"
      >
        {t('governance.mcp.env.editHeading')}
      </h4>

      <ul className="flex flex-col gap-3">
        {rows.map((row) => {
          const state = deriveState(row);
          const fieldId = `env-${serverName}-${row.key}`;
          return (
            <li key={row.key} className="flex flex-col gap-1">
              <span className="flex items-center justify-between gap-2">
                <label
                  htmlFor={fieldId}
                  className="break-all font-mono text-[13px] font-semibold text-text"
                >
                  {row.key}
                </label>
                <span className={`flex items-center gap-1 text-[13px] ${stateTone(state)}`}>
                  <span
                    aria-hidden="true"
                    className={`inline-block h-2 w-2 shrink-0 rounded-sm ${STATE_DOT[state]}`}
                  />
                  {t(STATE_LABEL[state])}
                </span>
              </span>
              <input
                id={fieldId}
                type="text"
                value={row.value}
                onChange={(event) => {
                  setRowValue(row.key, event.target.value);
                }}
                // A secret shows the redacted ${KEY} placeholder — NO eye-reveal, the value
                // was never sent. A missing required var shows an em-dash placeholder.
                placeholder={row.present ? undefined : '—'}
                aria-describedby={row.secret ? `${fieldId}-hint` : undefined}
                className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
              {row.secret ? (
                <span id={`${fieldId}-hint`} className="text-[13px] text-text-muted">
                  {t('governance.mcp.env.secretHint')}
                </span>
              ) : null}
            </li>
          );
        })}
      </ul>

      {offending.length > 0 ? (
        <div
          role="note"
          className="flex flex-col gap-1 rounded-md border border-warning bg-warning/10 px-3 py-2"
        >
          <p className="text-[13px] font-semibold text-warning">
            {t('governance.mcp.env.softWarning.heading')}
          </p>
          <p className="text-[15.5px] leading-relaxed text-text">
            {t('governance.mcp.env.softWarning.body')}
          </p>
          <ul className="flex flex-col gap-1">
            {offending.map((key) => (
              <li key={key} className="break-all font-mono text-[13px] text-warning">
                {key}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {mutation.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.error')}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={mutation.isPending}
          aria-busy={mutation.isPending}
          onClick={submit}
          className="inline-flex min-h-[44px] items-center justify-center gap-2 rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-wait disabled:opacity-60"
        >
          {mutation.isPending ? <Spinner /> : null}
          {t('governance.mcp.env.save')}
        </button>
        <button
          type="button"
          disabled={mutation.isPending}
          onClick={onClose}
          className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
          {t('governance.mcp.env.discard')}
        </button>
      </div>
    </section>
  );
}

function stateTone(state: EnvState): string {
  if (state === 'missing') return 'text-danger';
  if (state === 'placeholder') return 'text-warning';
  if (state === 'optional') return 'text-text-muted';
  // required (present): default text — signals "needs attention", distinct from muted optional
  // (UI-SPEC §2 four-state table: required label = `text`).
  return 'text-text';
}

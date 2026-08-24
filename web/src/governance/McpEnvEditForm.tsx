import { useId, useMemo, useState, type ComponentProps } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { setMcpServerEnv, type McpEnvChip } from './governanceApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

// McpEnvEditForm (MCPW-02): the four-state redacted env editor that replaces the read-only
// env-keys section of McpServerDetail in edit mode. It renders the four env states
// (required / optional / missing / placeholder) as a dot + label (never color-alone), masks
// secret rows by default, and surfaces a soft warning when a required var is still
// missing/placeholder at save. The save remains allowed (informational, MCPW-02 / D-F2).
//
// SECURITY (T-29-04-01): no raw secret value ever enters the DOM. A stored secret row's
// initial value is the redacted ${KEY} placeholder, not the value; the value is sent only when
// the operator types a real replacement. Submitting the unchanged placeholder preserves the
// stored secret through the backend SetServerEnv four-state merge.

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

function placeholderToken(key: string): string {
  return `\${${key}}`;
}

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
  readonly envKeys: readonly McpEnvChip[];
  readonly requiredEnv?: readonly string[];
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
      onSaved?.(preserved);
      onClose();
    },
  });

  function setRowValue(key: string, value: string) {
    setRows((prev) => prev.map((row) => (row.key === key ? { ...row, value } : row)));
  }

  // Rows are seeded from the keys a server ALREADY has plus the ones its recipe declares, so
  // a custom server — which is every hosted connector installed by URL — opens this form
  // empty with nothing to fill in. That is not a cosmetic gap: Slack's authorization server
  // publishes no registration_endpoint (measured 2026-08-24), so its client id and secret can
  // never be issued automatically and MUST be typed. Without this control there was nowhere
  // in the cockpit to type them.
  const [newKey, setNewKey] = useState('');
  const newKeyId = `env-${serverName}-new`;
  const normalizedNewKey = newKey.trim().toUpperCase();
  const newKeyValid =
    /^[A-Z_][A-Z0-9_]*$/.test(normalizedNewKey) &&
    !rows.some((row) => row.key === normalizedNewKey);

  // Removal is what makes the key name correctable at all: the name is typed, so it can be
  // typed wrong, and until now nothing in the cockpit could take one back out. Renaming is
  // remove + add, which is why there is no separate rename control.
  function removeRow(key: string) {
    setRows((prev) => prev.filter((row) => row.key !== key));
  }

  function addRow() {
    if (!newKeyValid) return;
    setRows((prev) => [
      ...prev,
      { key: normalizedNewKey, secret: true, required: false, present: false, value: '' },
    ]);
    setNewKey('');
  }

  const offending = rows
    .filter((row) => row.required)
    .filter((row) => !row.present || row.value === '' || row.value === placeholderToken(row.key))
    .map((row) => row.key);

  function submit() {
    const preserved = rows.some(
      (row) => row.secret && row.present && row.value === placeholderToken(row.key),
    );
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
                <Label
                  htmlFor={fieldId}
                  className="break-all font-mono text-[13px] font-semibold text-text"
                >
                  {row.key}
                </Label>
                <span className="flex items-center gap-1">
                  <Badge
                    variant={stateVariant(state)}
                    className={`text-[13px] ${stateTone(state)}`}
                  >
                    <span
                      aria-hidden="true"
                      className={`inline-block h-2 w-2 shrink-0 rounded-sm ${STATE_DOT[state]}`}
                    />
                    {t(STATE_LABEL[state])}
                  </Badge>
                  {/* A required key belongs to its recipe and cannot be deleted; anything
                      else can, which is the only way to undo a mistyped variable name. */}
                  {row.required ? null : (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t('governance.mcp.env.removeKey', { key: row.key })}
                      onClick={() => {
                        removeRow(row.key);
                      }}
                      className="text-text-muted hover:text-danger"
                    >
                      <Trash2 data-icon aria-hidden="true" className="size-4" />
                    </Button>
                  )}
                </span>
              </span>
              <EnvValueInput
                secret={row.secret}
                showLabel={t('secret.show', { label: row.key })}
                hideLabel={t('secret.hide', { label: row.key })}
                id={fieldId}
                type="text"
                value={row.value}
                onChange={(event) => {
                  setRowValue(row.key, event.target.value);
                }}
                placeholder={row.present ? undefined : '-'}
                aria-describedby={row.secret ? `${fieldId}-hint` : undefined}
                className="font-mono text-[13px]"
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

      <div className="flex flex-wrap items-end gap-2">
        <span className="flex min-w-[220px] flex-1 flex-col gap-1">
          <Label htmlFor={newKeyId} className="text-[13px] font-semibold text-text-muted">
            {t('governance.mcp.env.addKeyLabel')}
          </Label>
          <Input
            id={newKeyId}
            dir="ltr"
            value={newKey}
            placeholder="MCP_OAUTH_CLIENT_ID"
            onChange={(event) => {
              setNewKey(event.target.value);
            }}
            onKeyDown={(event) => {
              if (event.key !== 'Enter') return;
              event.preventDefault();
              addRow();
            }}
            className="font-mono text-[13px]"
          />
        </span>
        <Button type="button" variant="outline" disabled={!newKeyValid} onClick={addRow}>
          {t('governance.mcp.env.addKey')}
        </Button>
      </div>

      {offending.length > 0 ? (
        <Card role="note" className="gap-1 border-warning bg-warning/10 px-3 py-2">
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
        </Card>
      ) : null}

      {mutation.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('governance.error')}</AlertDescription>
        </Alert>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          disabled={mutation.isPending}
          aria-busy={mutation.isPending}
          onClick={submit}
        >
          {mutation.isPending ? <Spinner /> : null}
          {t('governance.mcp.env.save')}
        </Button>
        <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>
          {t('governance.mcp.env.discard')}
        </Button>
      </div>
    </section>
  );
}

interface EnvValueInputProps extends ComponentProps<'input'> {
  readonly secret: boolean;
  readonly showLabel: string;
  readonly hideLabel: string;
}

function EnvValueInput({ hideLabel, secret, showLabel, type, ...props }: EnvValueInputProps) {
  if (secret) {
    return <SecretInput {...props} showLabel={showLabel} hideLabel={hideLabel} />;
  }
  return <Input {...props} type={type} />;
}

function stateTone(state: EnvState): string {
  if (state === 'missing') return 'text-danger';
  if (state === 'placeholder') return 'text-warning';
  if (state === 'optional') return 'text-text-muted';
  return 'text-text';
}

function stateVariant(state: EnvState): 'secondary' | 'warning' | 'danger' {
  if (state === 'missing') return 'danger';
  if (state === 'placeholder') return 'warning';
  return 'secondary';
}

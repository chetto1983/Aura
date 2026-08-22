import { useId } from 'react';
import { ChevronRight, ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { ariaInvalid } from '../a11y/aria';
import { PIM_PROVIDERS, type PimFieldDef, type PimProviderDef } from './pimProviders';
import type { PimGoogleStart, PimProviderId } from './pimApi';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { SecretInput } from '@/components/ui/secret-input';

// CalendarConnectFields holds the presentational form controls + result panels for the calendar/PIM
// connect wizard, split out of CalendarConnect.tsx to keep that file under the 600-LOC cap. These are
// pure (state-free) components driven entirely by props; the stateful AddAccountForm orchestrator
// stays in CalendarConnect.tsx and composes them.

export function ProviderSelect({
  id,
  value,
  onChange,
}: {
  readonly id: string;
  readonly value: PimProviderId;
  readonly onChange: (value: PimProviderId) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id} className="text-[13px] font-semibold text-text">
        {t('governance.mcp.calendar.providerLabel')}
      </Label>
      <NativeSelect
        id={id}
        value={value}
        onChange={(event) => {
          onChange(event.target.value as PimProviderId);
        }}
        className="w-full bg-surface-3 text-[13px]"
      >
        {PIM_PROVIDERS.map((p) => (
          <NativeSelectOption key={p.id} value={p.id}>
            {t(p.labelKey)}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </div>
  );
}

export function ProviderConfigFields({
  def,
  values,
  onChange,
  submitted,
  missing,
}: {
  readonly def: PimProviderDef;
  readonly values: Record<string, string>;
  readonly onChange: (key: string, value: string) => void;
  readonly submitted: boolean;
  readonly missing: ReadonlySet<string>;
}) {
  const { t } = useTranslation();
  return (
    <>
      {def.fields.map((field) => {
        if (field.showIf !== undefined && values[field.showIf.key] !== field.showIf.value) {
          return null;
        }
        const invalid = submitted && missing.has(field.key);
        if (field.type === 'select' && field.options) {
          return (
            <SelectField
              key={field.key}
              field={field}
              value={values[field.key] ?? ''}
              onChange={(v) => {
                onChange(field.key, v);
              }}
            />
          );
        }
        return (
          <Field
            key={field.key}
            id={`pim-${def.id}-${field.key}`}
            label={t(field.labelKey)}
            value={values[field.key] ?? ''}
            onChange={(v) => {
              onChange(field.key, v);
            }}
            invalid={invalid}
            type={field.type === 'password' ? 'password' : 'text'}
            placeholder={field.placeholder}
            hint={field.hintKey !== undefined ? t(field.hintKey) : undefined}
          />
        );
      })}
    </>
  );
}

export function SelectField({
  field,
  value,
  onChange,
}: {
  readonly field: PimFieldDef;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  const id = `pim-select-${field.key}`;
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id} className="text-[13px] font-semibold text-text">
        {t(field.labelKey)}
      </Label>
      <NativeSelect
        id={id}
        value={value}
        onChange={(event) => {
          onChange(event.target.value);
        }}
        className="w-full bg-surface-3 text-[13px]"
      >
        {field.options?.map((opt) => (
          <NativeSelectOption key={opt.value} value={opt.value}>
            {t(opt.labelKey)}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </div>
  );
}

export function AdvancedSection({
  open,
  onToggle,
  domains,
  onDomains,
  priority,
  onPriority,
}: {
  readonly open: boolean;
  readonly onToggle: () => void;
  readonly domains: string;
  readonly onDomains: (value: string) => void;
  readonly priority: string;
  readonly onPriority: (value: string) => void;
}) {
  const { t } = useTranslation();
  const domainsId = useId();
  const priorityId = useId();
  return (
    <div className="flex flex-col gap-2">
      <Button
        type="button"
        variant="ghost"
        onClick={onToggle}
        aria-expanded={open}
        className="self-start px-2 text-[13px] text-text-muted hover:bg-transparent hover:underline"
      >
        <ChevronRight
          data-icon
          aria-hidden="true"
          className={`transition-transform motion-reduce:transition-none ${open ? 'rotate-90' : ''}`}
        />
        {t('governance.mcp.calendar.advanced.toggle')}
      </Button>
      {open ? (
        <>
          <Field
            id={domainsId}
            label={t('governance.mcp.calendar.advanced.domainsLabel')}
            value={domains}
            onChange={onDomains}
            invalid={false}
            hint={t('governance.mcp.calendar.advanced.domainsHint')}
          />
          <Field
            id={priorityId}
            label={t('governance.mcp.calendar.advanced.priorityLabel')}
            value={priority}
            onChange={onPriority}
            invalid={false}
            hint={t('governance.mcp.calendar.advanced.priorityHint')}
          />
        </>
      ) : null}
    </div>
  );
}

// StartFailedPanel surfaces the partial-success case: the account WAS created but the connect step
// didn't start. It offers a retry that re-runs only the connect step (never re-creating the account),
// so the operator isn't dead-ended on the sidecar's 409-duplicate.
export function StartFailedPanel({
  pending,
  onRetry,
}: {
  readonly pending: boolean;
  readonly onRetry: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3">
      <p role="status" className="text-[13px] text-warning">
        {t('governance.mcp.calendar.startFailed')}
      </p>
      <Button
        type="button"
        onClick={onRetry}
        disabled={pending}
        aria-busy={pending}
        className="self-start text-[13px] disabled:cursor-wait"
      >
        {pending ? <Spinner /> : null}
        {t('governance.mcp.calendar.retryConnect')}
      </Button>
    </div>
  );
}

export function GoogleStartPanel({ start }: { readonly start: PimGoogleStart }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3">
      <p className="text-[13px] font-semibold text-text">
        {t('governance.mcp.calendar.redirectHeading')}
      </p>
      <p className="break-all font-mono text-[13px] text-text">{start.redirectUri}</p>
      <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.redirectHint')}</p>
      <Button asChild className="self-start text-[13px]">
        <a href={start.authUrl} target="_blank" rel="noopener noreferrer">
          <ExternalLink data-icon aria-hidden="true" />
          {t('governance.mcp.calendar.connectGoogle')}
        </a>
      </Button>
      <p role="note" className="text-[13px] text-text-muted">
        {t('governance.mcp.calendar.consentNote')}
      </p>
    </div>
  );
}

export function Field({
  id,
  label,
  value,
  onChange,
  invalid,
  hint,
  error,
  type = 'text',
  placeholder,
}: {
  readonly id: string;
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly invalid: boolean;
  readonly hint?: string | undefined;
  /** Replaces the default "required" text when the field fails a rule other than emptiness. */
  readonly error?: string | undefined;
  readonly type?: 'text' | 'password';
  readonly placeholder?: string | undefined;
}) {
  const { t } = useTranslation();
  const errId = `${id}-err`;
  const hintId = `${id}-hint`;
  const describedBy = [invalid ? errId : null, hint !== undefined ? hintId : null]
    .filter((entry): entry is string => entry !== null)
    .join(' ');
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id} className="text-[13px] font-semibold text-text">
        {label}
      </Label>
      {type === 'password' ? (
        <SecretInput
          id={id}
          value={value}
          placeholder={placeholder}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          showLabel={t('secret.show', { label })}
          hideLabel={t('secret.hide', { label })}
          aria-invalid={ariaInvalid(invalid)}
          aria-describedby={describedBy === '' ? undefined : describedBy}
          className="bg-surface-3 font-mono text-[13px]"
        />
      ) : (
        <Input
          id={id}
          type={type}
          value={value}
          placeholder={placeholder}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          aria-invalid={ariaInvalid(invalid)}
          aria-describedby={describedBy === '' ? undefined : describedBy}
          className="bg-surface-3 font-mono text-[13px]"
        />
      )}
      {hint !== undefined ? (
        <span id={hintId} className="text-[13px] text-text-muted">
          {hint}
        </span>
      ) : null}
      {invalid ? (
        <p id={errId} role="alert" className="text-[13px] text-danger">
          {error ?? t('governance.mcp.calendar.required')}
        </p>
      ) : null}
    </div>
  );
}

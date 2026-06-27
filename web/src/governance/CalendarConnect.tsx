import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { ariaInvalid } from '../a11y/aria';
import { PimDeviceCodePanel } from './PimDeviceCodePanel';
import {
  PIM_PROVIDERS,
  pimInitialValues,
  pimMissingRequired,
  pimProviderById,
  pimSubmitConfig,
  type PimFieldDef,
  type PimProviderDef,
} from './pimProviders';
import {
  createPimAccount,
  deletePimAccount,
  listPimAccounts,
  pimDeviceStart,
  pimGoogleStart,
  type PimAccount,
  type PimCreateAccountRequest,
  type PimDeviceStart,
  type PimGoogleStart,
  type PimProviderId,
} from './pimApi';

// CalendarConnect — the inline calendar/PIM connect section the MCP server detail renders for the
// aura-pim-mcp server. It lists the configured accounts (~10s poll) each with a Disconnect button,
// and an "Add account" form driven by pimProviders.ts so the operator can configure EVERY provider
// the sidecar exposes (operator directive 2026-06-27: "configure all variable on frontend not just
// google") — not only Google's clientId/clientSecret. The provider picker swaps the visible config
// fields; on create the wizard routes the connect step by the provider's authFlow: Google opens the
// web-redirect consent (GoogleStartPanel), Microsoft/Outlook render the device-code grant
// (PimDeviceCodePanel), and credential/URL providers (IMAP/ICS/JSON) are ready immediately. All copy
// via governance.mcp.calendar.* (en + it); a 503 (sidecar unconfigured) → a calm offline note.

const ACCOUNTS_POLL_MS = 10000;

export function CalendarConnect() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();

  const accounts = useQuery({
    queryKey: ['connect', 'pim', 'accounts'],
    queryFn: listPimAccounts,
    refetchInterval: ACCOUNTS_POLL_MS,
    retry: false,
  });

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ['connect', 'pim', 'accounts'] });
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <h4
        id={headingId}
        className="text-[13px] font-semibold uppercase tracking-wide text-text-muted"
      >
        {t('governance.mcp.calendar.heading')}
      </h4>

      {accounts.isLoading ? (
        <p role="status" className="text-[15.5px] text-warning">
          {t('governance.mcp.calendar.loading')}
        </p>
      ) : accounts.isError ? (
        <p role="note" className="text-[15.5px] text-warning">
          {t('governance.mcp.calendar.offline')}
        </p>
      ) : (
        <AccountList accounts={accounts.data?.accounts ?? []} onChanged={refresh} />
      )}

      <AddAccountForm onCreated={refresh} />
    </section>
  );
}

function AccountList({
  accounts,
  onChanged,
}: {
  readonly accounts: readonly PimAccount[];
  readonly onChanged: () => void;
}) {
  const { t } = useTranslation();
  const remove = useMutation({
    mutationFn: (id: string) => deletePimAccount(id),
    onSuccess: onChanged,
  });

  if (accounts.length === 0) {
    return <p className="text-[15.5px] text-text-muted">{t('governance.mcp.calendar.empty')}</p>;
  }

  return (
    <div className="flex flex-col gap-2">
      <p className="text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.calendar.accountsHeading')}
      </p>
      <ul className="flex flex-col gap-2">
        {accounts.map((acct) => (
          <li
            key={acct.id}
            className="flex items-center justify-between gap-2 rounded-md border border-border bg-surface-2 px-3 py-2"
          >
            <span className="flex min-w-0 flex-col">
              <span className="break-words text-[15.5px] text-text">
                {acct.displayName || acct.id}
              </span>
              <span className="break-all font-mono text-[13px] text-text-muted">
                {acct.provider ? `${acct.provider} · ${acct.id}` : acct.id}
              </span>
            </span>
            <button
              type="button"
              disabled={remove.isPending}
              aria-busy={remove.isPending}
              onClick={() => {
                remove.mutate(acct.id);
              }}
              className="inline-flex min-h-[44px] shrink-0 items-center justify-center gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              {remove.isPending ? <Spinner /> : null}
              {t('governance.mcp.calendar.disconnect')}
            </button>
          </li>
        ))}
      </ul>
      {remove.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.mcp.calendar.disconnectError')}
        </p>
      ) : null}
    </div>
  );
}

type CreateResult =
  | { readonly kind: 'google'; readonly start: PimGoogleStart }
  | { readonly kind: 'device'; readonly start: PimDeviceStart }
  | { readonly kind: 'none' };

function parseDomains(raw: string): readonly string[] {
  return raw
    .split(',')
    .map((d) => d.trim())
    .filter((d) => d !== '');
}

function parsePriority(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === '') return undefined;
  const n = Number.parseInt(trimmed, 10);
  return Number.isNaN(n) ? undefined : n;
}

function AddAccountForm({ onCreated }: { readonly onCreated: () => void }) {
  const { t } = useTranslation();
  const ids = { provider: useId(), accountId: useId(), displayName: useId() };

  const [providerId, setProviderId] = useState<PimProviderId>('google');
  const def = pimProviderById(providerId);

  const [accountId, setAccountId] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [values, setValues] = useState<Record<string, string>>(() => pimInitialValues(def));
  const [domains, setDomains] = useState('');
  const [priority, setPriority] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [result, setResult] = useState<CreateResult | null>(null);

  function selectProvider(next: PimProviderId) {
    const nextDef = pimProviderById(next);
    setProviderId(next);
    setValues(pimInitialValues(nextDef));
    setResult(null);
    setSubmitted(false);
  }

  const accountIdEmpty = accountId.trim() === '';
  const displayNameEmpty = displayName.trim() === '';
  const missingConfig = new Set(pimMissingRequired(def, values));
  const hasEmpty = accountIdEmpty || displayNameEmpty || missingConfig.size > 0;

  const create = useMutation({
    mutationFn: async (): Promise<CreateResult> => {
      const id = accountId.trim();
      const domainList = parseDomains(domains);
      const prio = parsePriority(priority);
      const body: PimCreateAccountRequest = {
        id,
        displayName: displayName.trim(),
        provider: providerId,
        providerConfig: pimSubmitConfig(def, values),
        ...(domainList.length > 0 ? { domains: domainList } : {}),
        ...(prio !== undefined ? { priority: prio } : {}),
      };
      await createPimAccount(body);
      onCreated();
      if (def.authFlow === 'google') return { kind: 'google', start: await pimGoogleStart(id) };
      if (def.authFlow === 'device') return { kind: 'device', start: await pimDeviceStart(id) };
      return { kind: 'none' };
    },
    onSuccess: setResult,
  });

  function submit() {
    setSubmitted(true);
    if (hasEmpty) return;
    create.mutate();
  }

  const createdId = accountId.trim();

  return (
    <form
      className="flex flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <p className="text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.calendar.addHeading')}
      </p>

      <ProviderSelect id={ids.provider} value={providerId} onChange={selectProvider} />

      <Field
        id={ids.accountId}
        label={t('governance.mcp.calendar.accountIdLabel')}
        value={accountId}
        onChange={setAccountId}
        invalid={submitted && accountIdEmpty}
        hint={t('governance.mcp.calendar.accountIdHint')}
      />
      <Field
        id={ids.displayName}
        label={t('governance.mcp.calendar.displayNameLabel')}
        value={displayName}
        onChange={setDisplayName}
        invalid={submitted && displayNameEmpty}
      />

      <ProviderConfigFields
        def={def}
        values={values}
        onChange={(key, v) => {
          setValues((prev) => ({ ...prev, [key]: v }));
        }}
        submitted={submitted}
        missing={missingConfig}
      />

      <AdvancedSection
        open={showAdvanced}
        onToggle={() => {
          setShowAdvanced((o) => !o);
        }}
        domains={domains}
        onDomains={setDomains}
        priority={priority}
        onPriority={setPriority}
      />

      <button
        type="submit"
        disabled={create.isPending}
        aria-busy={create.isPending}
        className="inline-flex min-h-[44px] items-center justify-center gap-2 self-start rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-wait disabled:opacity-60"
      >
        {create.isPending ? <Spinner /> : null}
        {t('governance.mcp.calendar.submit')}
      </button>

      {create.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.error')}
        </p>
      ) : null}

      {result?.kind === 'google' ? <GoogleStartPanel start={result.start} /> : null}
      {result?.kind === 'device' ? (
        <PimDeviceCodePanel accountId={createdId} start={result.start} />
      ) : null}
      {result?.kind === 'none' ? (
        <p role="status" className="text-[13px] text-success">
          {t('governance.mcp.calendar.noAuthCreated')}
        </p>
      ) : null}
    </form>
  );
}

function ProviderSelect({
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
      <label htmlFor={id} className="text-[13px] font-semibold text-text">
        {t('governance.mcp.calendar.providerLabel')}
      </label>
      <select
        id={id}
        value={value}
        onChange={(event) => {
          onChange(event.target.value as PimProviderId);
        }}
        className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {PIM_PROVIDERS.map((p) => (
          <option key={p.id} value={p.id}>
            {t(p.labelKey)}
          </option>
        ))}
      </select>
    </div>
  );
}

function ProviderConfigFields({
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

function SelectField({
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
      <label htmlFor={id} className="text-[13px] font-semibold text-text">
        {t(field.labelKey)}
      </label>
      <select
        id={id}
        value={value}
        onChange={(event) => {
          onChange(event.target.value);
        }}
        className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {field.options?.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {t(opt.labelKey)}
          </option>
        ))}
      </select>
    </div>
  );
}

function AdvancedSection({
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
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="self-start text-[13px] font-semibold text-text-muted underline-offset-2 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
      >
        {open ? '▾ ' : '▸ '}
        {t('governance.mcp.calendar.advanced.toggle')}
      </button>
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

function GoogleStartPanel({ start }: { readonly start: PimGoogleStart }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3">
      <p className="text-[13px] font-semibold text-text">
        {t('governance.mcp.calendar.redirectHeading')}
      </p>
      <p className="break-all font-mono text-[13px] text-text">{start.redirectUri}</p>
      <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.redirectHint')}</p>
      <a
        href={start.authUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex min-h-[44px] items-center justify-center gap-2 self-start rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {t('governance.mcp.calendar.connectGoogle')}
      </a>
      <p role="note" className="text-[13px] text-text-muted">
        {t('governance.mcp.calendar.consentNote')}
      </p>
    </div>
  );
}

function Field({
  id,
  label,
  value,
  onChange,
  invalid,
  hint,
  type = 'text',
  placeholder,
}: {
  readonly id: string;
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly invalid: boolean;
  readonly hint?: string | undefined;
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
      <label htmlFor={id} className="text-[13px] font-semibold text-text">
        {label}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        placeholder={placeholder}
        onChange={(event) => {
          onChange(event.target.value);
        }}
        aria-invalid={ariaInvalid(invalid)}
        aria-describedby={describedBy === '' ? undefined : describedBy}
        className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
      {hint !== undefined ? (
        <span id={hintId} className="text-[13px] text-text-muted">
          {hint}
        </span>
      ) : null}
      {invalid ? (
        <p id={errId} role="alert" className="text-[13px] text-danger">
          {t('governance.mcp.calendar.required')}
        </p>
      ) : null}
    </div>
  );
}

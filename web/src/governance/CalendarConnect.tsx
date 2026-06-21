import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { ariaInvalid } from '../a11y/aria';
import {
  createPimAccount,
  deletePimAccount,
  listPimAccounts,
  pimGoogleStart,
  type PimAccount,
  type PimGoogleStart,
} from './governanceApi';

// CalendarConnect — the inline "Connect Google Calendar" section the MCP server detail renders when
// the selected server is the calendar (aura-pim-mcp) one. It lists the configured accounts (~10s
// poll), each with a Disconnect button (DELETE), and an "Add Google account" form where the operator
// enters their OWN Google OAuth clientId/clientSecret (nothing from env). On create it immediately
// mints the Google consent URL (pimGoogleStart) and shows the exact redirect URI to register + a
// "Connect Google" link that opens the consent URL in a new tab. Per the verified contract there is
// NO reliable Google "is it linked" signal, so the v1 UX treats "account exists" as configured and
// instructs the operator to finish consent in the opened tab, then Refresh. All copy via the
// governance.mcp.calendar.* i18n keys (en + it); a 503 (sidecar unconfigured) → a calm offline note.

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
              <span className="break-all font-mono text-[13px] text-text-muted">{acct.id}</span>
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

function AddAccountForm({ onCreated }: { readonly onCreated: () => void }) {
  const { t } = useTranslation();
  const ids = {
    accountId: useId(),
    displayName: useId(),
    clientId: useId(),
    clientSecret: useId(),
  };

  const [accountId, setAccountId] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const [started, setStarted] = useState<PimGoogleStart | null>(null);

  const empties = {
    accountId: accountId.trim() === '',
    displayName: displayName.trim() === '',
    clientId: clientId.trim() === '',
    clientSecret: clientSecret.trim() === '',
  };
  const hasEmpty = empties.accountId || empties.displayName || empties.clientId || empties.clientSecret;

  const create = useMutation({
    mutationFn: async () => {
      await createPimAccount({
        id: accountId.trim(),
        displayName: displayName.trim(),
        provider: 'google',
        providerConfig: { clientId: clientId.trim(), clientSecret: clientSecret.trim() },
      });
      onCreated();
      return pimGoogleStart(accountId.trim());
    },
    onSuccess: (start: PimGoogleStart) => {
      setStarted(start);
    },
  });

  function submit() {
    setSubmitted(true);
    if (hasEmpty) return;
    create.mutate();
  }

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

      <Field
        id={ids.accountId}
        label={t('governance.mcp.calendar.accountIdLabel')}
        value={accountId}
        onChange={setAccountId}
        invalid={submitted && empties.accountId}
        hint={t('governance.mcp.calendar.accountIdHint')}
      />
      <Field
        id={ids.displayName}
        label={t('governance.mcp.calendar.displayNameLabel')}
        value={displayName}
        onChange={setDisplayName}
        invalid={submitted && empties.displayName}
      />
      <Field
        id={ids.clientId}
        label={t('governance.mcp.calendar.clientIdLabel')}
        value={clientId}
        onChange={setClientId}
        invalid={submitted && empties.clientId}
      />
      <Field
        id={ids.clientSecret}
        label={t('governance.mcp.calendar.clientSecretLabel')}
        value={clientSecret}
        onChange={setClientSecret}
        invalid={submitted && empties.clientSecret}
        type="password"
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

      {started !== null ? <GoogleStartPanel start={started} /> : null}
    </form>
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
}: {
  readonly id: string;
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly invalid: boolean;
  readonly hint?: string;
  readonly type?: 'text' | 'password';
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

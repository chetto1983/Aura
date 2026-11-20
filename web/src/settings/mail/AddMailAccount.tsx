import { useId, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { HttpError } from '../../api/json';
import { Field, ProviderConfigFields } from '../../governance/CalendarConnectFields';
import { PimConnectPanel } from '../../governance/PimConnectPanel';
import { startConnect, type PimConnectResult } from '../../governance/pimConnect';
import {
  pimAccountFlow,
  pimAccountIdError,
  pimMissingRequired,
  pimProviderById,
  type PimAuthFlow,
  type PimProviderDef,
} from '../../governance/pimProviders';
import {
  deletePimAccount,
  discoverPim,
  patchPimProviderConfig,
  pimErrorKey,
  pimVerifyFailure,
  PIM_PROVIDER_NOT_CONFIGURED,
  type PimAccount,
  type PimDiscovery,
} from '../../governance/pimApi';
import {
  accountIdFromAddress,
  httpsUrl,
  isServerFailure,
  isValidAddress,
  mailProviderConfig,
  serverValuesFrom,
  SERVER_KEYS,
  verifyMessageKey,
  type ServerValues,
} from './mailConnectLogic';
import {
  createMailAccount,
  isOAuthKind,
  type MailCreateRequest,
  type MailOutcome,
  type OAuthKind,
} from './mailAccountCreate';
import { OtherAccountForm } from './OtherAccountForm';
import { Button } from '@/components/ui/button';

// AddMailAccount connects a mailbox from its address and password. Leaving the address asks the PIM
// what it knows about the domain: Google and Microsoft get their own sign-in in place of the
// password, anything else keeps the password and a closed «Impostazioni server» disclosure pre-filled
// by discovery. «Collega» creates the account only after the PIM has signed in to it; a refusal lands
// on the field it concerns. Connecting does not turn the mail watch on: that is the row's switch.

const TIMEOUT_KEY = 'settings.mail.error.timeout';
const SMTP_WARNINGS = new Set(['smtp_unreachable', 'tls_failed']);
const IMAP_DEF = pimProviderById('imap');
const EMPTY_SERVERS: ServerValues = {
  imapHost: '',
  imapPort: '',
  smtpHost: '',
  smtpPort: '',
  username: '',
};

interface Failure {
  readonly field: 'password' | 'server' | 'form';
  readonly key: string;
}

/** The five server fields of the IMAP form; for an unknown domain the hosts get a guess as their
 * placeholder, never as their value. */
function serverDef(unknownDomain: string | null): PimProviderDef {
  const fields = IMAP_DEF.fields
    .filter((field) => (SERVER_KEYS as readonly string[]).includes(field.key))
    .map((field) => {
      if (unknownDomain === null) return field;
      if (field.key === 'imapHost') return { ...field, placeholder: `imap.${unknownDomain}` };
      if (field.key === 'smtpHost') return { ...field, placeholder: `smtp.${unknownDomain}` };
      return field;
    });
  return { ...IMAP_DEF, fields };
}

function isNotConfigured(error: unknown): boolean {
  return error instanceof HttpError && error.reason === PIM_PROVIDER_NOT_CONFIGURED;
}

export function AddMailAccount({ onCreated }: { readonly onCreated: () => void }) {
  const { t } = useTranslation();
  const ids = { address: useId(), password: useId() };

  const [address, setAddress] = useState('');
  const [password, setPassword] = useState('');
  const [found, setFound] = useState<{ address: string; discovery: PimDiscovery } | null>(null);
  const [servers, setServers] = useState<ServerValues>(EMPTY_SERVERS);
  const [serverOpen, setServerOpen] = useState(false);
  const [appPasswordChosen, setAppPasswordChosen] = useState(false);
  const [appMissing, setAppMissing] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [timedOut, setTimedOut] = useState(false);
  const [outcome, setOutcome] = useState<MailOutcome | null>(null);
  const [signIn, setSignIn] = useState<PimConnectResult | null>(null);
  const [other, setOther] = useState(false);

  const trimmed = address.trim();
  const current = found?.address === trimmed ? found.discovery : null;
  const oauthKind: OAuthKind | null =
    current !== null && isOAuthKind(current.kind) && !appPasswordChosen ? current.kind : null;
  const appPasswordRequired = current?.appPassword?.required === true;
  const helpUrl = appPasswordRequired ? httpsUrl(current.appPassword.helpUrl) : null;
  const mxHost = current?.source?.endsWith('-mx') === true ? current.imapHost : undefined;
  const domain = current?.kind === 'unknown' ? (trimmed.split('@')[1] ?? null) : null;
  const def = serverDef(domain);
  const missingServers = new Set(pimMissingRequired(def, servers));
  const addressUsable =
    isValidAddress(trimmed) && pimAccountIdError(accountIdFromAddress(trimmed, 1)) === null;

  function startOver(next: MailOutcome | null) {
    setOutcome(next);
    setFailure(null);
    setAppMissing(false);
    setSignIn(null);
    setTimedOut(false);
  }

  const discovery = useMutation({
    mutationFn: (email: string) =>
      discoverPim(email).catch((): PimDiscovery => ({ kind: 'unknown' })),
    onSuccess: (result, email) => {
      setFound({ address: email, discovery: result });
      setServers(serverValuesFrom(result, email));
      setServerOpen(result.kind === 'unknown');
      setAppPasswordChosen(false);
      startOver(null);
    },
  });

  function connected(account: PimAccount, smtpWarning?: string) {
    startOver({ kind: 'created', account, smtpWarning });
    setAddress('');
    setPassword('');
    setFound(null);
    setServers(EMPTY_SERVERS);
    setServerOpen(false);
    setSubmitted(false);
    onCreated();
  }

  function showFailure(error: unknown) {
    const verify = pimVerifyFailure(error);
    if (verify === null) {
      const key = pimErrorKey(error);
      setTimedOut(key === TIMEOUT_KEY);
      setFailure({ field: 'form', key });
      return;
    }
    const field = isServerFailure(verify) ? 'server' : 'password';
    if (field === 'server') setServerOpen(true);
    setFailure({ field, key: verifyMessageKey(verify, appPasswordRequired) });
  }

  function settle(result: MailOutcome) {
    if (result.kind === 'created') connected(result.account, result.smtpWarning);
    else startOver(result);
  }

  // Every input rides in the mutation's variables: onMutate re-renders before mutationFn runs, and
  // TanStack hands the running mutation that render's options, so a closure would read reset state.
  const connectMail = useMutation({
    mutationFn: createMailAccount,
    onMutate: () => {
      startOver(null);
    },
    onSuccess: settle,
    onError: showFailure,
  });

  const oauthSignIn = useMutation({
    mutationFn: async (request: MailCreateRequest & { readonly provider: OAuthKind }) => {
      const result = await createMailAccount(request);
      if (result.kind === 'existing') return { result, connect: null };
      onCreated();
      const flow = pimProviderById(request.provider).authFlow;
      return { result, connect: await startConnect(result.account.id, flow) };
    },
    onMutate: () => {
      startOver(null);
    },
    onSuccess: ({ result, connect }) => {
      if (connect === null) setOutcome(result);
      else setSignIn(connect);
    },
    onError: (error) => {
      if (isNotConfigured(error)) setAppMissing(true);
      else showFailure(error);
    },
  });

  const retryStart = useMutation({
    mutationFn: ({ id, flow }: { id: string; flow: PimAuthFlow }) => startConnect(id, flow),
    onSuccess: setSignIn,
  });

  const changePassword = useMutation({
    mutationFn: ({ id, secret }: { id: string; secret: string }) =>
      patchPimProviderConfig(id, { password: secret }),
    onSuccess: ({ smtpWarning, ...account }) => {
      connected(account, smtpWarning);
    },
    onError: showFailure,
  });

  const remove = useMutation({
    mutationFn: (id: string) => deletePimAccount(id),
    onSuccess: () => {
      startOver(null);
      onCreated();
    },
    onError: showFailure,
  });

  function discover() {
    if (!isValidAddress(trimmed) || found?.address === trimmed) return;
    if (discovery.isPending && discovery.variables === trimmed) return;
    discovery.mutate(trimmed);
  }

  function submit() {
    setSubmitted(true);
    if (!addressUsable) return;
    if (current === null) {
      discover();
      return;
    }
    if (oauthKind !== null || password === '') return;
    if (missingServers.size > 0) {
      setServerOpen(true);
      return;
    }
    connectMail.mutate({
      address: trimmed,
      provider: 'imap',
      providerConfig: mailProviderConfig(servers, password, current),
      recheck: timedOut,
    });
  }

  const busy = discovery.isPending || connectMail.isPending;
  const passwordFailure = failure?.field === 'password' ? failure : null;
  const providerLabel = oauthKind === null ? '' : t(pimProviderById(oauthKind).labelKey);

  if (other) {
    return (
      <div className="flex flex-col gap-3">
        <OtherToggle
          open
          onToggle={() => {
            setOther(false);
          }}
        />
        <OtherAccountForm onCreated={onCreated} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <form
        noValidate
        className="flex flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        {outcome?.kind === 'created' ? (
          <ConnectedNotice account={outcome.account} smtpWarning={outcome.smtpWarning} />
        ) : null}

        <Field
          id={ids.address}
          type="email"
          label={t('settings.mail.addressLabel')}
          value={address}
          onChange={setAddress}
          onBlur={discover}
          invalid={submitted && !addressUsable}
          error={t('settings.mail.error.invalidAddress')}
        />
        {discovery.isPending ? (
          <p role="status" className="text-[13px] text-text-muted">
            {t('settings.mail.discovering')}
          </p>
        ) : null}

        {oauthKind !== null ? (
          signIn === null ? (
            <OAuthSignIn
              kind={oauthKind}
              providerLabel={providerLabel}
              appMissing={appMissing}
              passwordFallback={current?.passwordFallback === true}
              pending={oauthSignIn.isPending}
              onSignIn={() => {
                oauthSignIn.mutate({
                  address: trimmed,
                  provider: oauthKind,
                  providerConfig: {},
                  recheck: timedOut,
                });
              }}
              onUseAppPassword={() => {
                setAppPasswordChosen(true);
                setAppMissing(false);
              }}
            />
          ) : null
        ) : (
          <>
            <Field
              id={ids.password}
              type="password"
              label={t('settings.mail.passwordLabel')}
              value={password}
              onChange={(next) => {
                setPassword(next);
                if (passwordFailure !== null) setFailure(null);
              }}
              invalid={(submitted && password === '') || passwordFailure !== null}
              error={passwordFailure === null ? undefined : t(passwordFailure.key)}
            />
            {appPasswordRequired ? (
              <p role="note" className="text-[13px] text-warning">
                {t('settings.mail.appPasswordNotice')}{' '}
                {helpUrl === null ? null : (
                  <a
                    href={helpUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-info underline"
                  >
                    {t('settings.mail.appPasswordHelp')}
                  </a>
                )}
              </p>
            ) : null}
            {mxHost === undefined ? null : (
              <p role="note" className="text-[13px] text-text-muted">
                {t('settings.mail.mxNotice', { host: mxHost })}
              </p>
            )}
            <details
              open={serverOpen}
              onToggle={(event) => {
                setServerOpen(event.currentTarget.open);
              }}
              className="rounded-md border border-border px-3 py-2"
            >
              <summary className="cursor-pointer text-[13px] font-semibold text-text-muted">
                {t('settings.mail.serverSettings')}
              </summary>
              <div className="mt-3 flex flex-col gap-3">
                <ProviderConfigFields
                  def={def}
                  values={servers}
                  onChange={(key, value) => {
                    setServers((prev) => ({ ...prev, [key]: value }));
                    if (failure?.field === 'server') setFailure(null);
                  }}
                  submitted={submitted}
                  missing={missingServers}
                />
                {failure?.field === 'server' ? (
                  <p role="alert" className="text-[13px] text-danger">
                    {t(failure.key)}
                  </p>
                ) : null}
              </div>
            </details>
            <Button
              type="submit"
              disabled={busy}
              aria-busy={connectMail.isPending}
              className="self-start text-[13px] disabled:cursor-wait"
            >
              {connectMail.isPending ? <Spinner /> : null}
              {t(connectMail.isPending ? 'settings.mail.connecting' : 'settings.mail.connect')}
            </Button>
          </>
        )}

        {failure?.field === 'form' ? (
          <p role="alert" className="text-[13px] text-danger">
            {t(failure.key)}
          </p>
        ) : null}

        {outcome?.kind === 'existing' ? (
          <ExistingAccount
            account={outcome.account}
            canChangePassword={password !== ''}
            pending={changePassword.isPending || remove.isPending}
            onChangePassword={() => {
              changePassword.mutate({ id: outcome.account.id, secret: password });
            }}
            onRemove={() => {
              remove.mutate(outcome.account.id);
            }}
          />
        ) : null}

        {signIn === null ? null : (
          <PimConnectPanel
            result={signIn}
            retrying={retryStart.isPending}
            onRetry={() => {
              if (signIn.kind === 'startFailed') {
                retryStart.mutate({ id: signIn.id, flow: signIn.authFlow });
              }
            }}
          />
        )}
      </form>
      <OtherToggle
        open={false}
        onToggle={() => {
          setOther(true);
        }}
      />
    </div>
  );
}

function OtherToggle({
  open,
  onToggle,
}: {
  readonly open: boolean;
  readonly onToggle: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Button
      type="button"
      variant="ghost"
      aria-expanded={open}
      onClick={onToggle}
      className="self-start px-2 text-[13px] text-text-muted hover:bg-transparent hover:underline"
    >
      {t('settings.mail.otherAccountType')}
    </Button>
  );
}

function OAuthSignIn({
  kind,
  providerLabel,
  appMissing,
  passwordFallback,
  pending,
  onSignIn,
  onUseAppPassword,
}: {
  readonly kind: OAuthKind;
  readonly providerLabel: string;
  readonly appMissing: boolean;
  readonly passwordFallback: boolean;
  readonly pending: boolean;
  readonly onSignIn: () => void;
  readonly onUseAppPassword: () => void;
}) {
  const { t } = useTranslation();
  if (appMissing) {
    return passwordFallback ? (
      <div className="flex flex-col gap-2">
        <p role="note" className="text-[13px] text-warning">
          {t('settings.mail.providerAppMissing', { provider: providerLabel })}
        </p>
        <Button
          type="button"
          variant="outline"
          onClick={onUseAppPassword}
          className="self-start text-[13px]"
        >
          {t('settings.mail.useAppPassword')}
        </Button>
      </div>
    ) : (
      <p role="note" className="text-[13px] text-warning">
        {t('settings.mail.providerAppMissingNoFallback', { provider: providerLabel })}
      </p>
    );
  }
  return (
    <Button
      type="button"
      onClick={onSignIn}
      disabled={pending}
      aria-busy={pending}
      className="self-start text-[13px] disabled:cursor-wait"
    >
      {pending ? <Spinner /> : null}
      {t(kind === 'google' ? 'settings.mail.signIn.google' : 'settings.mail.signIn.microsoft')}
    </Button>
  );
}

function ConnectedNotice({
  account,
  smtpWarning,
}: {
  readonly account: PimAccount;
  readonly smtpWarning: string | undefined;
}) {
  const { t } = useTranslation();
  return (
    <div
      role="status"
      className="flex flex-col gap-1 rounded-md border border-border bg-surface-2 px-3 py-2"
    >
      <span className="text-[13px] text-success">{t('settings.mail.connected')}</span>
      <span className="break-words text-[15.5px] text-text">
        {account.displayName || account.id}
      </span>
      {smtpWarning !== undefined && SMTP_WARNINGS.has(smtpWarning) ? (
        <span className="text-[13px] text-warning">
          {t(`settings.mail.verify.smtpWarning.${smtpWarning}`)}
        </span>
      ) : null}
    </div>
  );
}

function ExistingAccount({
  account,
  canChangePassword,
  pending,
  onChangePassword,
  onRemove,
}: {
  readonly account: PimAccount;
  readonly canChangePassword: boolean;
  readonly pending: boolean;
  readonly onChangePassword: () => void;
  readonly onRemove: () => void;
}) {
  const { t } = useTranslation();
  const leftover = pimAccountFlow(account.provider) !== 'none' && account.linked !== true;
  return (
    <div role="alert" className="flex flex-col gap-2">
      <p className="text-[13px] text-warning">
        {t(leftover ? 'settings.mail.conflict.leftover' : 'settings.mail.conflict.sameAddress')}
      </p>
      {account.provider === 'imap' ? (
        <Button
          type="button"
          variant="outline"
          disabled={pending || !canChangePassword}
          onClick={onChangePassword}
          className="self-start text-[13px]"
        >
          {t('settings.mail.changePassword')}
        </Button>
      ) : null}
      {leftover ? (
        <Button
          type="button"
          variant="outline"
          disabled={pending}
          onClick={onRemove}
          className="self-start text-[13px]"
        >
          {t('settings.mail.remove')}
        </Button>
      ) : null}
    </div>
  );
}

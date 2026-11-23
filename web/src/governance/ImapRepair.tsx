import { useId, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  failureField,
  repairConfig,
  repairMessageKey,
  REPAIR_SERVER_KEYS,
  serversTyped,
  smtpWarningKey,
  type RepairServers,
} from '../settings/mail/mailConnectLogic';
import { Field, ProviderConfigFields } from './CalendarConnectFields';
import { refreshRepairedAccount } from './mailWatch/useMailWatch';
import {
  patchPimProviderConfig,
  pimErrorKey,
  pimVerifyFailure,
  verifyPimAccount,
  type PimAccount,
} from './pimApi';
import { pimProviderById, type PimProviderDef } from './pimProviders';
import { Spinner } from '@/components/Spinner';
import { Button } from '@/components/ui/button';

// ImapRepair is the repair on an IMAP account's row, on the same account id so the watch keeps its
// place. «Prova di nuovo» checks the stored login the way the watch reads; «Cambia password» sends a
// new password, and any server field typed under «Impostazioni server» (blank means unchanged). The
// username is not offered: another mailbox is another account (A3). A working login lets the server
// resume the watch and read at once, so the row only refreshes. Whether that read then works is not
// judged here: the status says when a run was claimed, not when its read ended, so the sentence
// tells the person what a failure at the next check would mean, and the buttons stay.

const IMAP_DEF = pimProviderById('imap');
const SERVER_DEF: PimProviderDef = {
  ...IMAP_DEF,
  fields: IMAP_DEF.fields
    .filter((field) => (REPAIR_SERVER_KEYS as readonly string[]).includes(field.key))
    .map((field) => ({ ...field, required: false })),
};
const BLANK: RepairServers = { imapHost: '', imapPort: '', smtpHost: '', smtpPort: '' };
const NO_MISSING: ReadonlySet<string> = new Set();

type Outcome =
  | { readonly kind: 'works'; readonly smtpWarning: string | undefined }
  | { readonly kind: 'failed'; readonly key: string };

export function ImapRepair({
  account,
  onWorked,
}: {
  readonly account: PimAccount;
  /** Called once a login has been shown to work, so the row stops asking for a new password. */
  readonly onWorked: () => void;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const passwordId = useId();
  const [editing, setEditing] = useState(false);
  const [password, setPassword] = useState('');
  const [servers, setServers] = useState<RepairServers>(BLANK);
  const [serverOpen, setServerOpen] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);

  function closeForm() {
    setEditing(false);
    setPassword('');
    setServers(BLANK);
    setServerOpen(false);
    setSubmitted(false);
  }

  function started() {
    setOutcome(null);
  }

  function worked({ smtpWarning }: { smtpWarning?: string }) {
    setOutcome({ kind: 'works', smtpWarning });
    closeForm();
    onWorked();
    refreshRepairedAccount(queryClient);
  }

  function failed(error: unknown) {
    const verify = pimVerifyFailure(error);
    if (failureField(error) === 'server') setServerOpen(true);
    setOutcome({
      kind: 'failed',
      key: verify === null ? pimErrorKey(error) : repairMessageKey(verify),
    });
  }

  const retry = useMutation({
    mutationFn: () => verifyPimAccount(account.id),
    onMutate: started,
    onSuccess: worked,
    onError: failed,
  });

  // The config holds the password: gcTime 0 drops it as soon as the row stops observing it.
  const save = useMutation({
    mutationFn: (config: Record<string, string>) => patchPimProviderConfig(account.id, config),
    gcTime: 0,
    onMutate: started,
    onSuccess: worked,
    onError: failed,
  });

  function submit() {
    setSubmitted(true);
    const config = repairConfig(servers, password);
    if (config !== null) save.mutate(config);
  }

  const busy = retry.isPending || save.isPending;
  const passwordMissing = submitted && password === '';
  const warningKey = outcome?.kind === 'works' ? smtpWarningKey(outcome.smtpWarning) : null;

  return (
    <div className="flex flex-col gap-2">
      <span className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={busy}
          aria-busy={retry.isPending}
          onClick={() => {
            retry.mutate();
          }}
          className="px-3 text-[13px]"
        >
          {retry.isPending ? <Spinner /> : null}
          {t('settings.mail.repair.tryAgain')}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={busy || editing}
          aria-expanded={editing}
          onClick={() => {
            setEditing(true);
            setOutcome(null);
          }}
          className="px-3 text-[13px]"
        >
          {t('settings.mail.changePassword')}
        </Button>
      </span>

      {editing ? (
        <form
          noValidate
          className="flex flex-col gap-3 rounded-md border border-border px-3 py-3"
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
        >
          <Field
            id={passwordId}
            type="password"
            label={t('settings.mail.repair.newPassword')}
            value={password}
            onChange={setPassword}
            invalid={passwordMissing}
            error={
              passwordMissing && serversTyped(servers)
                ? t('settings.mail.error.passwordRequired')
                : undefined
            }
          />
          <p className="text-[13px] text-text-muted">{t('settings.mail.repair.usernameFixed')}</p>
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
              <p className="text-[13px] text-text-muted">{t('settings.mail.repair.serverHint')}</p>
              <ProviderConfigFields
                def={SERVER_DEF}
                values={servers}
                onChange={(key, value) => {
                  setServers((prev) => ({ ...prev, [key]: value }));
                }}
                submitted={false}
                missing={NO_MISSING}
              />
            </div>
          </details>
          <span className="flex flex-wrap gap-2">
            <Button
              type="submit"
              disabled={busy}
              aria-busy={save.isPending}
              className="px-3 text-[13px] disabled:cursor-wait"
            >
              {save.isPending ? <Spinner /> : null}
              {t('settings.mail.repair.save')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={save.isPending}
              onClick={closeForm}
              className="px-3 text-[13px]"
            >
              {t('settings.mail.repair.cancel')}
            </Button>
          </span>
        </form>
      ) : null}

      {outcome?.kind === 'works' ? (
        <p role="status" className="flex flex-col text-[13px]">
          <span className="text-success">{t('settings.mail.repair.loginWorks')}</span>
          {warningKey === null ? null : <span className="text-warning">{t(warningKey)}</span>}
        </p>
      ) : null}
      {outcome?.kind === 'failed' ? (
        <p role="alert" className="text-[13px] text-danger">
          {t(outcome.key)}
        </p>
      ) : null}
    </div>
  );
}

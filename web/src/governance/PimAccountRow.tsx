import { useMutation } from '@tanstack/react-query';
import { TriangleAlert } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AccountWatch } from './mailWatch/AccountWatch';
import { startConnect } from './pimConnect';
import { PimConnectPanel } from './PimConnectPanel';
import { pimAccountFlow } from './pimProviders';
import type { PimAccount } from './pimApi';
import { Spinner } from '@/components/Spinner';
import { Button } from '@/components/ui/button';

// PimAccountRow is one configured account. An account is listed from the moment it is added, before
// anyone signs in, so the row reads `linked` before it says anything: one whose sign-in never
// finished offers to finish it, a connected one to sign in again, both on the same account. Removing
// is the only way to forget the account, so it says what the mail watch loses and asks first: a
// "Disconnect" that removed the account wiped its watch history on every re-link (2026-11-02 and
// 2026-11-03, lab VM).

export function PimAccountRow({
  account,
  removing,
  onRemove,
}: {
  readonly account: PimAccount;
  readonly removing: boolean;
  readonly onRemove: () => void;
}) {
  const { t } = useTranslation();
  const flow = pimAccountFlow(account.provider);
  const connect = useMutation({ mutationFn: () => startConnect(account.id, flow) });
  const [confirming, setConfirming] = useState(false);
  const unfinished = account.linked === false;

  return (
    <li className="flex flex-col gap-2 rounded-md border border-border bg-surface-2 px-3 py-2">
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 flex-col">
          <span className="break-words text-[15.5px] text-text">
            {account.displayName || account.id}
          </span>
          <span className="break-all font-mono text-[13px] text-text-muted">
            {account.provider ? `${account.provider} · ${account.id}` : account.id}
          </span>
        </span>
        <span className="flex shrink-0 flex-wrap justify-end gap-2">
          {flow !== 'none' ? (
            <Button
              type="button"
              variant={unfinished ? 'default' : 'outline'}
              disabled={connect.isPending}
              aria-busy={connect.isPending}
              onClick={() => {
                connect.mutate();
              }}
              className="px-3 text-[13px]"
            >
              {connect.isPending ? <Spinner /> : null}
              {t(
                unfinished
                  ? 'governance.mcp.calendar.finishSignIn'
                  : 'governance.mcp.calendar.signInAgain',
              )}
            </Button>
          ) : null}
          <Button
            type="button"
            variant="outline"
            disabled={removing || confirming}
            onClick={() => {
              setConfirming(true);
            }}
            className="px-3 text-[13px]"
          >
            {t('governance.mcp.calendar.remove')}
          </Button>
        </span>
      </div>
      {confirming ? (
        <div
          role="group"
          aria-label={t('governance.mcp.calendar.remove')}
          className="flex flex-col gap-2 rounded-md border border-danger px-3 py-2"
        >
          <p className="flex items-start gap-1.5 text-[13px] text-danger">
            <TriangleAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
            <span>{t('governance.mcp.calendar.removeConfirm')}</span>
          </p>
          <span className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="ghost"
              disabled={removing}
              aria-busy={removing}
              onClick={onRemove}
              className="border border-danger px-3 text-[13px] text-danger hover:bg-danger/10 hover:text-danger"
            >
              {removing ? <Spinner /> : null}
              {t('governance.mcp.calendar.removeForGood')}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={removing}
              onClick={() => {
                setConfirming(false);
              }}
              className="px-3 text-[13px]"
            >
              {t('governance.mcp.calendar.cancel')}
            </Button>
          </span>
        </div>
      ) : null}
      {unfinished ? (
        <p role="status" className="flex items-start gap-1.5 text-[13px] text-warning">
          <TriangleAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          <span>{t('governance.mcp.calendar.notLinked')}</span>
        </p>
      ) : null}
      {connect.data !== undefined ? (
        <PimConnectPanel
          result={connect.data}
          retrying={connect.isPending}
          signingInAgain={!unfinished}
          onRetry={() => {
            connect.mutate();
          }}
        />
      ) : null}
      <AccountWatch account={account} />
    </li>
  );
}

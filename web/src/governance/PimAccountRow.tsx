import { useMutation } from '@tanstack/react-query';
import { TriangleAlert } from 'lucide-react';
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
// finished offers to finish it or to remove it, and draws no "Disconnect" for a connection that does
// not exist.

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
          {unfinished && flow !== 'none' ? (
            <Button
              type="button"
              disabled={connect.isPending}
              aria-busy={connect.isPending}
              onClick={() => {
                connect.mutate();
              }}
              className="px-3 text-[13px]"
            >
              {connect.isPending ? <Spinner /> : null}
              {t('governance.mcp.calendar.finishSignIn')}
            </Button>
          ) : null}
          <Button
            type="button"
            variant="outline"
            disabled={removing}
            aria-busy={removing}
            onClick={onRemove}
            className="px-3 text-[13px]"
          >
            {removing ? <Spinner /> : null}
            {t(
              unfinished ? 'governance.mcp.calendar.remove' : 'governance.mcp.calendar.disconnect',
            )}
          </Button>
        </span>
      </div>
      {unfinished ? (
        <p role="status" className="flex items-start gap-1.5 text-[13px] text-warning">
          <TriangleAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          <span>{t('governance.mcp.calendar.notLinked')}</span>
        </p>
      ) : null}
      {unfinished && connect.data !== undefined ? (
        <PimConnectPanel
          result={connect.data}
          retrying={connect.isPending}
          onRetry={() => {
            connect.mutate();
          }}
        />
      ) : null}
      <AccountWatch account={account} />
    </li>
  );
}

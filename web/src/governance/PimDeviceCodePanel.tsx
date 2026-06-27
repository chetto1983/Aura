import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { pimAuthStatus, type PimDeviceStart } from './pimApi';

// PimDeviceCodePanel renders the Microsoft/Outlook device-code grant after the wizard creates a
// device-flow account and calls pimDeviceStart. It shows the user code prominently + a link to the
// Microsoft verification URL (new tab), and polls …/{id}/auth/status until the flow settles. Per the
// sidecar contract the status is one of pending | awaiting_user | completed | failed | cancelled |
// not_found; only the non-terminal states keep polling. All copy via governance.mcp.calendar.device.*.

const AUTH_POLL_MS = 4000;
const TERMINAL = new Set(['completed', 'failed', 'cancelled', 'not_found']);

export function PimDeviceCodePanel({
  accountId,
  start,
}: {
  readonly accountId: string;
  readonly start: PimDeviceStart;
}) {
  const { t } = useTranslation();

  const status = useQuery({
    queryKey: ['connect', 'pim', 'auth', accountId],
    queryFn: () => pimAuthStatus(accountId),
    refetchInterval: (query) =>
      TERMINAL.has(query.state.data?.status ?? '') ? false : AUTH_POLL_MS,
    retry: false,
  });

  const state = status.data?.status ?? 'pending';
  // A transport/HTTP error (e.g. 503 sidecar offline) leaves status.data undefined, which would
  // otherwise render a perpetual "waiting" spinner; treat it as a settled non-completed state so the
  // panel surfaces the failure instead of hanging.
  const settled = status.isError || TERMINAL.has(state);

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3">
      <p className="text-[13px] font-semibold text-text">
        {t('governance.mcp.calendar.device.heading')}
      </p>

      <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.device.codeLabel')}</p>
      <p className="select-all break-all font-mono text-[22px] font-semibold tracking-[0.18em] text-text">
        {start.userCode}
      </p>

      <a
        href={start.verificationUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex min-h-[44px] items-center justify-center gap-2 self-start rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {t('governance.mcp.calendar.device.openVerification')}
      </a>
      <p className="break-all font-mono text-[13px] text-text-muted">{start.verificationUrl}</p>
      <p role="note" className="text-[13px] text-text-muted">
        {t('governance.mcp.calendar.device.instructions')}
      </p>

      {/* Live device-flow state — the wizard polls until the grant settles. */}
      {!settled ? (
        <p role="status" className="flex items-center gap-2 text-[13px] text-warning">
          <Spinner />
          {t('governance.mcp.calendar.device.awaitingUser')}
        </p>
      ) : state === 'completed' ? (
        <p role="status" className="text-[13px] text-success">
          {t('governance.mcp.calendar.device.linked')}
        </p>
      ) : (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.mcp.calendar.device.failed')}
        </p>
      )}
    </div>
  );
}

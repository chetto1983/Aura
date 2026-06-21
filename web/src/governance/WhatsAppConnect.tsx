import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import {
  WHATSAPP_CONNECT_QR_PATH,
  whatsappConnectLogout,
  whatsappConnectStatus,
} from './governanceApi';

// WhatsAppConnect — the inline "Link device" section the MCP server detail renders when the
// selected server is the WhatsApp one. It polls the connect status (4s); when paired/connected
// it shows the JID (mono) + an Unlink button (POST logout → invalidate), when waiting_qr it
// shows the server-rendered QR PNG re-fetched every ~3s (a tick state cache-busts the <img>
// src), and on a status-query error it shows a graceful bridge-offline note. All copy via the
// governance.mcp.connect.* i18n keys (en + it). The QR is a backend-rendered image (no frontend
// QR dependency); credentials/same-origin are handled by the governanceApi helpers.

const QR_TICK_MS = 3000;
const STATUS_POLL_MS = 4000;

export function WhatsAppConnect() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [tick, setTick] = useState(0);

  const status = useQuery({
    queryKey: ['connect', 'whatsapp', 'status'],
    queryFn: whatsappConnectStatus,
    refetchInterval: STATUS_POLL_MS,
    retry: false,
  });

  const linked = status.data?.connected === true || status.data?.paired === true;
  const waitingQR = status.data !== undefined && !linked;

  // Advance the QR cache-bust tick every ~3s ONLY while waiting for a scan. The interval is
  // cleared on unmount and whenever the waiting state ends (no leaked timer, mini-PC budget).
  useEffect(() => {
    if (!waitingQR) return undefined;
    const id = window.setInterval(() => {
      setTick((n) => n + 1);
    }, QR_TICK_MS);
    return () => {
      window.clearInterval(id);
    };
  }, [waitingQR]);

  const logout = useMutation({
    mutationFn: whatsappConnectLogout,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['connect', 'whatsapp', 'status'] });
    },
  });

  return (
    <section className="flex flex-col gap-2">
      <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
        {t('governance.mcp.connect.heading')}
      </h4>

      {status.isLoading ? (
        <p role="status" className="text-[15.5px] text-warning">
          {t('governance.mcp.connect.checking')}
        </p>
      ) : status.isError ? (
        <p role="note" className="text-[15.5px] text-warning">
          {t('governance.mcp.connect.offline')}
        </p>
      ) : linked ? (
        <div className="flex flex-col gap-2">
          <p role="status" className="text-[15.5px] text-success">
            {t('governance.mcp.connect.connected')}
          </p>
          {status.data !== undefined && status.data.jid !== '' ? (
            <p className="break-all font-mono text-[13px] text-text">{status.data.jid}</p>
          ) : null}
          <button
            type="button"
            onClick={() => {
              logout.mutate();
            }}
            disabled={logout.isPending}
            className="inline-flex min-h-[44px] items-center justify-center gap-2 self-start rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
          >
            {logout.isPending ? <Spinner /> : null}
            {t('governance.mcp.connect.unlink')}
          </button>
          {logout.isError ? (
            <p role="alert" className="text-[13px] text-danger">
              {t('governance.mcp.connect.unlinkError')}
            </p>
          ) : null}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <p className="text-[15.5px] text-text">{t('governance.mcp.connect.scanInstructions')}</p>
          <img
            src={`${WHATSAPP_CONNECT_QR_PATH}?ts=${String(tick)}`}
            alt={t('governance.mcp.connect.qrAlt')}
            width={264}
            height={264}
            className="self-start rounded-md border border-border bg-surface p-2"
          />
        </div>
      )}
    </section>
  );
}

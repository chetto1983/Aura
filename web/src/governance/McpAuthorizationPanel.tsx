import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Check, Copy, ExternalLink, Link2, Unlink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  MCP_FLOW_MAX_POLL_FAILURES,
  MCP_FLOW_POLL_MS,
  fetchMcpAuthorization,
  pollMcpAuthorizationFlow,
  revokeMcpAuthorization,
  startMcpAuthorization,
} from './mcpAuthApi';
import { Button } from '@/components/ui/button';

// McpAuthorizationPanel — the cockpit's Connect control for a remote MCP server that
// authenticates a PERSON rather than the deployment.
//
// The affordances follow LibreChat's McpOAuthDialog: once a consent URL exists the human
// gets more than one way to reach it — continue in this browser, or copy the link and
// finish somewhere else — because the cockpit is often open on a machine that is not the
// one holding the password manager, and because a popup blocker can swallow the tab.
//
// The waiting half is Hermes's dashboard loop (start, then poll until approved or error,
// absorbing a few consecutive failures rather than reading one dropped request as an
// abandoned authorization). Here that loop is react-query's refetchInterval + retry,
// which is how WhatsAppConnect already polls on this same board.

export interface McpAuthorizationPanelProps {
  readonly serverName: string;
}

export function McpAuthorizationPanel({ serverName }: McpAuthorizationPanelProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [flowId, setFlowId] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const authorization = useQuery({
    queryKey: ['mcp-authorization', serverName],
    queryFn: () => fetchMcpAuthorization(serverName),
  });

  const flow = useQuery({
    queryKey: ['mcp-authorization-flow', flowId],
    queryFn: () => pollMcpAuthorizationFlow(flowId ?? ''),
    enabled: flowId !== null,
    // Polling stops the moment the flow resolves either way. A spinner that never ends
    // is worse than an error: it never tells anyone what to do next.
    refetchInterval: (query) =>
      query.state.data?.status === 'authorization_required' ? MCP_FLOW_POLL_MS : false,
    retry: MCP_FLOW_MAX_POLL_FAILURES,
  });

  const start = useMutation({
    mutationFn: () => startMcpAuthorization(serverName),
    onSuccess: (started) => {
      setFlowId(started.flowId);
      // Opened here, in the mutation's own callback, so the browser still counts it as
      // the user gesture that began the click. The copyable link below is what covers
      // the case where a blocker swallows it anyway.
      if (started.authorizationUrl) {
        window.open(started.authorizationUrl, '_blank', 'noopener,noreferrer');
      }
    },
  });

  const revoke = useMutation({
    mutationFn: () => revokeMcpAuthorization(serverName),
    onSuccess: async () => {
      setFlowId(null);
      await queryClient.invalidateQueries({ queryKey: ['mcp-authorization', serverName] });
    },
  });

  const live = flow.data ?? start.data;
  const approved = live?.status === 'approved';

  // The one thing the poll cannot express by itself: once the redirect has landed, the
  // STORED state is what the panel must render, so it has to be re-read. Only an
  // invalidation happens here — no state is set — so the render stays a function of the
  // queries.
  useEffect(() => {
    if (!approved) return;
    void queryClient.invalidateQueries({ queryKey: ['mcp-authorization', serverName] });
  }, [approved, queryClient, serverName]);

  const copyLink = async () => {
    if (!live?.authorizationUrl) return;
    try {
      await navigator.clipboard.writeText(live.authorizationUrl);
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, 2000);
    } catch {
      setCopied(false);
    }
  };

  const state = authorization.data;
  if (!state) return null;

  // A server that takes no authorization gets one muted line saying why, never a disabled
  // button with no explanation.
  if (!state.supported) {
    return (
      <section className="flex flex-col gap-1 rounded-lg border border-border p-3">
        <h4 className="text-[13px] font-semibold text-text-muted">
          {t('governance.mcp.detail.authorization.title')}
        </h4>
        <p className="text-[14px] text-text-muted">
          {state.reason ?? t('governance.mcp.detail.authorization.unsupported')}
        </p>
      </section>
    );
  }

  const waitingURL = live?.status === 'authorization_required' ? (live.authorizationUrl ?? '') : '';
  const failure =
    live?.status === 'error'
      ? (live.error ?? t('governance.mcp.detail.authorization.failed'))
      : (start.error?.message ?? revoke.error?.message ?? '');

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-border p-3">
      <h4 className="text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.detail.authorization.title')}
      </h4>

      {state.authorized && !waitingURL ? (
        <>
          <p className="text-[14px] text-text">
            {t('governance.mcp.detail.authorization.authorized')}
            {state.expiresAt ? ` · ${new Date(state.expiresAt).toLocaleString()}` : ''}
          </p>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={revoke.isPending}
              onClick={() => {
                revoke.mutate();
              }}
            >
              <Unlink data-icon aria-hidden="true" className="size-4" />
              {t('governance.mcp.detail.authorization.disconnect')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={start.isPending}
              onClick={() => {
                start.mutate();
              }}
            >
              {t('governance.mcp.detail.authorization.reconnect')}
            </Button>
          </div>
        </>
      ) : null}

      {!state.authorized && !waitingURL ? (
        <>
          <p className="text-[14px] text-text-muted">
            {t('governance.mcp.detail.authorization.notAuthorized')}
          </p>
          <div>
            <Button
              type="button"
              variant="default"
              size="sm"
              disabled={start.isPending}
              onClick={() => {
                start.mutate();
              }}
            >
              <Link2 data-icon aria-hidden="true" className="size-4" />
              {start.isPending
                ? t('governance.mcp.detail.authorization.starting')
                : t('governance.mcp.detail.authorization.connect')}
            </Button>
          </div>
        </>
      ) : null}

      {waitingURL ? (
        <div className="flex flex-col gap-2">
          <p className="text-[14px] text-text">
            {t('governance.mcp.detail.authorization.waiting')}
          </p>
          {/* The URL is shown, not merely opened: a blocked popup would otherwise leave
              no way forward, and the cockpit is frequently not on the machine that can
              complete the consent. */}
          <input
            readOnly
            dir="ltr"
            value={waitingURL}
            aria-label={t('governance.mcp.detail.authorization.copyLink')}
            onFocus={(event) => {
              event.currentTarget.select();
            }}
            className="w-full rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[12.5px] text-text-muted"
          />
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                window.open(waitingURL, '_blank', 'noopener,noreferrer');
              }}
            >
              <ExternalLink data-icon aria-hidden="true" className="size-4" />
              {t('governance.mcp.detail.authorization.openAgain')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                void copyLink();
              }}
            >
              {copied ? (
                <Check data-icon aria-hidden="true" className="size-4" />
              ) : (
                <Copy data-icon aria-hidden="true" className="size-4" />
              )}
              {t('governance.mcp.detail.authorization.copyLink')}
            </Button>
          </div>
        </div>
      ) : null}

      {failure ? (
        <p role="alert" className="break-words text-[13.5px] text-danger">
          {failure}
        </p>
      ) : null}
    </section>
  );
}

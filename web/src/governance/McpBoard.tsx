import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BoardLayout } from './BoardLayout';
import { BoardStateView, boardStatus } from './governanceView';
import { McpServerDetail } from './McpServerDetail';
import { fetchMcpServers, probeMcpServer, type McpServerRow } from './governanceApi';

// McpBoard (GOV-01) — the MCP-servers master list + detail. The static row list comes from one
// query (fetchMcpServers, always renders). CRITICALLY each row fires its OWN probe query keyed
// by server name (useMcpProbe in McpProbeStatus) so a hung/dead server resolves to its own
// "Timed out"/"Error" state WITHOUT blanking or stalling sibling rows (T-28-03-05 — per-row
// isolation). Secret env values never enter the DOM: only redacted KEY chips render (T-28-03-01).
// Selecting a row opens McpServerDetail (the live doctor result + tool list) as a desktop column
// / mobile bottom sheet.

// eslint-disable-next-line react-refresh/only-export-components
export function statusLabel(
  t: ReturnType<typeof useTranslation>['t'],
  probe: { ok: boolean; tool_count: number; detail: string; err?: string } | undefined,
  isLoading: boolean,
  isError: boolean,
): { text: string; tone: string } {
  if (isLoading) {
    return { text: t('governance.mcp.status.checking'), tone: 'text-warning' };
  }
  if (isError) {
    return { text: t('governance.mcp.status.timedOut'), tone: 'text-warning' };
  }
  if (probe === undefined) {
    return { text: t('governance.mcp.status.checking'), tone: 'text-warning' };
  }
  if (probe.ok) {
    return {
      text: t('governance.mcp.status.healthy', { count: probe.tool_count }),
      tone: 'text-success',
    };
  }
  // A reachable-but-failed probe (dial/list error) → the danger "Error — {state}" copy.
  return {
    text: t('governance.mcp.status.error', { state: probe.detail }),
    tone: 'text-danger',
  };
}

/** McpProbeStatus owns ONE row's live probe. Its own useQuery means a slow/hung server only
 * delays ITS status line — the list and every sibling row stay responsive (R1 isolation). The
 * resolved status renders in a role="status" live region so AT announces the transition. */
function McpProbeStatus({ name }: { readonly name: string }) {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useQuery({
    queryKey: ['governance', 'mcp', 'probe', name],
    queryFn: () => probeMcpServer(name),
    retry: false,
    staleTime: 30_000,
  });
  const { text, tone } = statusLabel(t, data, isLoading, isError);
  return (
    <span role="status" className={`text-[13px] font-semibold ${tone}`}>
      {text}
    </span>
  );
}

export function McpBoard() {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const rowRefs = useRef<(HTMLButtonElement | null)[]>([]);

  const servers = useQuery({
    queryKey: ['governance', 'mcp', 'list'],
    queryFn: fetchMcpServers,
    retry: false,
  });

  const rows: readonly McpServerRow[] = servers.data ?? [];
  const status = boardStatus({
    isLoading: servers.isLoading,
    isError: servers.isError,
    error: servers.error,
    isEmpty: rows.length === 0,
  });

  const selectedServer = rows.find((s) => s.name === selected);

  // The selected server's probe also feeds the detail pane (shared query key — TanStack dedupes).
  const detailProbe = useQuery({
    queryKey: ['governance', 'mcp', 'probe', selected ?? ''],
    queryFn: () => probeMcpServer(selected ?? ''),
    retry: false,
    enabled: selectedServer !== undefined,
    staleTime: 30_000,
  });

  function selectRow(name: string, el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(name);
  }

  function focusRow(index: number) {
    rowRefs.current[index]?.focus();
  }

  function onRowKeyDown(event: React.KeyboardEvent<HTMLButtonElement>, index: number) {
    if (rows.length === 0) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowRight') {
      event.preventDefault();
      focusRow((index + 1) % rows.length);
    } else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') {
      event.preventDefault();
      focusRow((index - 1 + rows.length) % rows.length);
    } else if (event.key === 'Home') {
      event.preventDefault();
      focusRow(0);
    } else if (event.key === 'End') {
      event.preventDefault();
      focusRow(rows.length - 1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      const server = rows[index];
      if (server !== undefined) {
        selectRow(server.name, event.currentTarget);
      }
    }
  }

  const master = (
    <div role="list" aria-label={t('governance.tabs.mcp')} className="flex flex-col gap-1 p-2">
      {rows.map((server, index) => (
        <div key={server.name} role="listitem">
          <button
            type="button"
            aria-pressed={selected === server.name}
            ref={(el) => {
              rowRefs.current[index] = el;
            }}
            onKeyDown={(e) => {
              onRowKeyDown(e, index);
            }}
            onClick={(e) => {
              selectRow(server.name, e.currentTarget);
            }}
            className={`flex min-h-[44px] w-full flex-col gap-1 rounded-md border px-3 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
              selected === server.name
                ? 'border-accent bg-accent text-on-accent'
                : 'border-border bg-surface-2 text-text hover:border-border-strong'
            }`}
          >
            <span className="flex items-center justify-between gap-2">
              {/* Backend-supplied name — React-escaped, mono (identifier). */}
              <span className="break-all font-mono text-[15.5px]">{server.name}</span>
              <span className="shrink-0 text-[13px] text-text-muted">{server.trust}</span>
            </span>
            <span className="flex items-center justify-between gap-2">
              <McpProbeStatus name={server.name} />
              {server.envKeys.length > 0 ? (
                <span className="shrink-0 rounded-sm bg-surface-3 px-1.5 py-0.5 font-mono text-[12px] text-text-muted">
                  {server.envKeys.length} · {t('governance.mcp.redacted')}
                </span>
              ) : server.source !== '' ? (
                <span className="shrink-0 rounded-sm bg-surface-3 px-1.5 py-0.5 font-mono text-[12px] text-text-muted">
                  {server.source}
                </span>
              ) : null}
            </span>
          </button>
        </div>
      ))}
    </div>
  );

  return (
    <BoardStateView
      status={status}
      emptyHeading={t('governance.mcp.emptyHeading')}
      emptyBody={t('governance.mcp.emptyBody')}
      onRetry={() => {
        void servers.refetch();
      }}
    >
      <BoardLayout
        master={master}
        detail={
          selectedServer !== undefined ? (
            <McpServerDetail
              server={selectedServer}
              probe={detailProbe.data}
              probeLoading={detailProbe.isLoading}
              onClose={() => {
                setSelected(undefined);
              }}
            />
          ) : undefined
        }
        detailOpen={selectedServer !== undefined}
        onCloseDetail={() => {
          setSelected(undefined);
        }}
        restoreFocusRef={restoreFocusRef}
        detailLabel={t('governance.detailEmpty')}
      />
    </BoardStateView>
  );
}

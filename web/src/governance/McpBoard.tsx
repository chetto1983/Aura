import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { BoardLayout } from './BoardLayout';
import { BoardStateView, boardStatus } from './governanceView';
import { McpInstallPanel } from './McpInstallPanel';
import { McpServerDetail } from './McpServerDetail';
import { fetchMcpServers, probeMcpServer, type McpServerRow } from './governanceApi';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';

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
  return {
    text: t('governance.mcp.status.error', { state: probe.detail }),
    tone: 'text-danger',
  };
}

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
  const [installing, setInstalling] = useState(false);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const addButtonRef = useRef<HTMLButtonElement | null>(null);
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

  function openInstall(el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(undefined);
    setInstalling(true);
  }

  function closeInstall() {
    setInstalling(false);
  }

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
    <div role="list" aria-label={t('governance.sections.mcp')} className="flex flex-col gap-2 p-2">
      {rows.map((server, index) => {
        const isSelected = selected === server.name;
        const selectedBadgeClass = isSelected
          ? 'border-on-accent/30 bg-on-accent/15 text-on-accent'
          : undefined;

        return (
          <Card
            key={server.name}
            role="listitem"
            className={`gap-0 overflow-hidden p-0 transition-colors ${
              isSelected ? 'border-accent' : 'hover:border-border-strong'
            }`}
          >
            <Button
              type="button"
              variant={isSelected ? 'default' : 'ghost'}
              aria-pressed={isSelected}
              ref={(el) => {
                rowRefs.current[index] = el;
              }}
              onKeyDown={(e) => {
                onRowKeyDown(e, index);
              }}
              onClick={(e) => {
                selectRow(server.name, e.currentTarget);
              }}
              className={`h-auto min-h-[52px] w-full flex-col items-stretch justify-start gap-2 px-3 py-2 text-left ${
                isSelected ? 'text-on-accent' : 'bg-transparent text-text hover:bg-surface-2'
              }`}
            >
              <span className="flex items-center justify-between gap-2">
                <span className="break-all font-mono text-[15.5px]">{server.name}</span>
                <Badge variant="secondary" className={selectedBadgeClass}>
                  {server.trust}
                </Badge>
              </span>
              <span className="flex items-center justify-between gap-2">
                <McpProbeStatus name={server.name} />
                {server.envKeys.length > 0 ? (
                  <Badge
                    variant="secondary"
                    className={`font-mono tabular-nums tracking-tight ${selectedBadgeClass ?? ''}`}
                  >
                    {server.envKeys.length} · {t('governance.mcp.redacted')}
                  </Badge>
                ) : server.source !== '' ? (
                  <Badge
                    variant="secondary"
                    className={`font-mono tracking-tight ${selectedBadgeClass ?? ''}`}
                  >
                    {server.source}
                  </Badge>
                ) : null}
              </span>
            </Button>
          </Card>
        );
      })}
    </div>
  );

  const installPanel = installing ? (
    <McpInstallPanel existingNames={rows.map((s) => s.name)} onClose={closeInstall} />
  ) : undefined;

  const detail =
    installPanel ??
    (selectedServer !== undefined ? (
      <McpServerDetail
        server={selectedServer}
        probe={detailProbe.data}
        probeLoading={detailProbe.isLoading}
        onClose={() => {
          setSelected(undefined);
        }}
      />
    ) : undefined);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center justify-end border-b border-border bg-surface px-2 py-1">
        <Button
          ref={addButtonRef}
          type="button"
          onClick={(e) => {
            openInstall(e.currentTarget);
          }}
        >
          <Plus data-icon="inline-start" aria-hidden="true" focusable="false" />
          {t('governance.mcp.addServer')}
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        {installing ? (
          <BoardLayout
            master={master}
            detail={installPanel}
            detailOpen={true}
            onCloseDetail={closeInstall}
            restoreFocusRef={restoreFocusRef}
            detailLabel={t('governance.detailEmpty')}
          />
        ) : (
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
              detail={detail}
              detailOpen={detail !== undefined}
              onCloseDetail={() => {
                setSelected(undefined);
                setInstalling(false);
              }}
              restoreFocusRef={restoreFocusRef}
              detailLabel={t('governance.detailEmpty')}
            />
          </BoardStateView>
        )}
      </div>
    </div>
  );
}

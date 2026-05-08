import { useCallback, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Database,
  ExternalLink,
  Globe,
  Loader2,
  LockKeyhole,
  Mail,
  Play,
  Plug,
  RefreshCw,
  Server,
  ShieldAlert,
  ShieldCheck,
  Wrench,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { ErrorCard } from '@/components/common/ErrorCard';
import type {
  ConnectorProbeResponse,
  ConnectorProviderSummary,
  ConnectorRiskBadge,
  MCPInvokeResponse,
  MCPServerSummary,
  MCPToolInfo,
} from '@/types/api';

// MCPPanel is now split into operator-facing connector configuration and the
// original raw MCP diagnostic surface. Raw tool invocation stays available, but
// provider setup starts from approved Aura connector profiles.
export function MCPPanel() {
  const { t } = useLocale();
  const fetchProviders = useCallback(() => api.mcpProviders(), []);
  const fetchServers = useCallback(() => api.mcpServers(), []);
  const providers = useApi(fetchProviders);
  const servers = useApi(fetchServers);
  const totalTools = (servers.data ?? []).reduce((acc, s) => acc + s.tool_count, 0);

  if ((providers.loading && !providers.data) || (servers.loading && !servers.data)) {
    return <MCPSkeleton />;
  }

  return (
    <div className="p-6 space-y-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">{t('mcp.title')}</h1>
          <p className="mt-1 max-w-3xl text-xs text-muted-foreground">
            {t('mcp.subtitle')}
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            void providers.refetch();
            void servers.refetch();
          }}
          aria-label={t('mcp.refresh')}
          title={t('mcp.refreshHint')}
          className="inline-flex min-h-11 items-center gap-2 rounded-md border px-3 py-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <RefreshCw size={14} />
          <span>{t('mcp.refresh')}</span>
          {t('mcp.providerCount', { count: providers.data?.length ?? 0 })} ·{' '}
          {t('mcp.serverCount', { count: servers.data?.length ?? 0 })} ·{' '}
          {t('mcp.toolCount', { count: totalTools })}
        </button>
      </header>

      <Tabs defaultValue="connectors" className="space-y-4">
        <TabsList className="h-auto flex-wrap justify-start">
          <TabsTrigger value="connectors" className="min-h-9 px-3">
            <Plug size={14} />
            {t('mcp.tab.connectors')}
          </TabsTrigger>
          <TabsTrigger value="installed" className="min-h-9 px-3">
            <CheckCircle2 size={14} />
            {t('mcp.tab.installed')}
          </TabsTrigger>
          <TabsTrigger value="health" className="min-h-9 px-3">
            <Activity size={14} />
            {t('mcp.tab.health')}
          </TabsTrigger>
          <TabsTrigger value="review" className="min-h-9 px-3">
            <ShieldCheck size={14} />
            {t('mcp.tab.review')}
          </TabsTrigger>
          <TabsTrigger value="raw" className="min-h-9 px-3">
            <Wrench size={14} />
            {t('mcp.tab.raw')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="connectors">
          <ConnectorsView
            providers={providers.data ?? []}
            error={providers.error ?? null}
            onRetry={providers.refetch}
          />
        </TabsContent>

        <TabsContent value="installed">
          <InstalledView providers={providers.data ?? []} />
        </TabsContent>

        <TabsContent value="health">
          <HealthView providers={providers.data ?? []} />
        </TabsContent>

        <TabsContent value="review">
          <EmptyTab icon={ShieldCheck} title={t('mcp.review.emptyTitle')} />
        </TabsContent>

        <TabsContent value="raw">
          <RawMCPView
            data={servers.data ?? []}
            error={servers.error ?? null}
            onRetry={servers.refetch}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function ConnectorsView({
  providers,
  error,
  onRetry,
}: {
  providers: ConnectorProviderSummary[];
  error: Error | null;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  if (error && providers.length === 0) {
    return <ErrorCard error={error} title={t('mcp.providers.errorTitle')} onRetry={onRetry} />;
  }
  if (providers.length === 0) {
    return (
      <div className="rounded-lg border border-dashed py-12 text-center">
        <div className="flex flex-col items-center gap-2 text-muted-foreground">
          <Plug size={32} className="opacity-40" />
          <p className="text-sm font-medium">{t('mcp.providers.emptyTitle')}</p>
        </div>
      </div>
    );
  }
  return (
    <div className="grid gap-3 xl:grid-cols-2">
      {providers.map((provider) => (
        <ConnectorCard key={provider.id} provider={provider} />
      ))}
    </div>
  );
}

function ConnectorCard({ provider }: { provider: ConnectorProviderSummary }) {
  const { t } = useLocale();
  const Icon = provider.kind === 'database' ? Database : Mail;
  const enabledCount = provider.capabilities.filter((c) => c.enabled).length;
  const [probe, setProbe] = useState<ConnectorProbeResponse | null>(null);
  const [probing, setProbing] = useState(false);

  const runProbe = async () => {
    setProbing(true);
    try {
      const result = await api.probeMCPProvider(provider.id);
      setProbe(result);
      if (result.ok) {
        toast.success(t('mcp.providers.probeOk', { provider: provider.name }));
      } else {
        toast.error(result.error || t('mcp.providers.probeFailed'));
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : t('mcp.providers.probeFailed');
      setProbe({ ok: false, provider_id: provider.id, error: message });
      toast.error(message);
    } finally {
      setProbing(false);
    }
  };

  return (
    <article className="rounded-lg border bg-card p-4">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <div className="grid size-10 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
            <Icon size={18} />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="truncate text-base font-semibold">{provider.name}</h2>
              <StatusPill status={provider.status} />
            </div>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {provider.description}
            </p>
          </div>
        </div>
        {provider.repository_url && (
          <a
            href={provider.repository_url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex size-9 shrink-0 items-center justify-center rounded-md border text-muted-foreground hover:bg-muted hover:text-foreground"
            title={t('mcp.providers.openRepository')}
            aria-label={t('mcp.providers.openRepository')}
          >
            <ExternalLink size={14} />
          </a>
        )}
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        <MetaPill label={provider.runtime_type} />
        <MetaPill label={t(`mcp.providers.profile.${provider.profile}`)} />
        <MetaPill label={t(`mcp.providers.kind.${provider.kind}`)} />
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        {provider.risk_badges.map((badge) => (
          <RiskPill key={badge.id} badge={badge} />
        ))}
      </div>

      <div className="mt-4 grid gap-3 lg:grid-cols-2">
        <section className="space-y-2">
          <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            {t('mcp.providers.capabilities')}
          </h3>
          <div className="space-y-1.5">
            {provider.capabilities.map((capability) => (
              <div key={capability.id} className="flex items-center justify-between gap-2 rounded-md border px-2 py-1.5 text-xs">
                <span className="min-w-0 truncate">{capability.label}</span>
                <span className="shrink-0 text-[10px] uppercase tracking-wide text-muted-foreground">
                  {capability.review_required ? t('mcp.providers.review') : t('mcp.providers.ready')}
                </span>
              </div>
            ))}
          </div>
        </section>

        <section className="space-y-2">
          <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            {t('mcp.providers.allowlist')}
          </h3>
          <ToolList values={provider.approved_tools ?? []} tone="approved" />
          <ToolList values={provider.blocked_tools ?? []} tone="blocked" />
        </section>
      </div>

      {provider.required_secrets && provider.required_secrets.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          <LockKeyhole size={13} />
          {provider.required_secrets.map((secret) => (
            <code key={secret} className="rounded bg-muted px-1.5 py-0.5 text-[11px]">
              {secret}
            </code>
          ))}
        </div>
      )}

      {probe && (
        <div
          className={`mt-3 rounded-md border px-3 py-2 text-xs ${
            probe.ok
              ? 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-200'
              : 'border-destructive/30 bg-destructive/5 text-destructive'
          }`}
        >
          <div className="flex flex-wrap items-center gap-2 font-medium">
            {probe.ok ? <CheckCircle2 size={13} /> : <AlertTriangle size={13} />}
            <span>{probe.ok ? t('mcp.providers.probeReady') : t('mcp.providers.probeNotReady')}</span>
          </div>
          {probe.capabilities_ready && probe.capabilities_ready.length > 0 && (
            <p className="mt-1 text-[11px]">
              {t('mcp.providers.readyCapabilities')}: {probe.capabilities_ready.join(', ')}
            </p>
          )}
          {probe.missing_capabilities && probe.missing_capabilities.length > 0 && (
            <p className="mt-1 text-[11px]">
              {t('mcp.providers.missingCapabilities')}: {probe.missing_capabilities.join(', ')}
            </p>
          )}
          {probe.blocked_tools_advertised && probe.blocked_tools_advertised.length > 0 && (
            <p className="mt-1 text-[11px]">
              {t('mcp.providers.blockedAdvertised')}: {probe.blocked_tools_advertised.join(', ')}
            </p>
          )}
          {probe.error && <p className="mt-1 text-[11px]">{probe.error}</p>}
        </div>
      )}

      <div className="mt-4 flex flex-wrap justify-between gap-2 border-t pt-3">
        <span className="text-xs text-muted-foreground">
          {t('mcp.providers.enabledCount', { enabled: enabledCount, total: provider.capabilities.length })}
        </span>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => void runProbe()}
            disabled={probing}
            className="inline-flex min-h-10 items-center gap-1.5 rounded-md border px-3 py-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-60"
            title={t('mcp.providers.probeHint')}
          >
            {probing ? <Loader2 size={13} className="animate-spin" /> : <Activity size={13} />}
            {t('mcp.providers.probe')}
          </button>
          <button
            type="button"
            disabled
            className="inline-flex min-h-10 items-center gap-1.5 rounded-md bg-primary px-3 py-2 text-xs text-primary-foreground opacity-60"
            title={t('mcp.providers.enableNext')}
          >
            <CheckCircle2 size={13} />
            {t('mcp.providers.enable')}
          </button>
        </div>
      </div>
    </article>
  );
}

function InstalledView({ providers }: { providers: ConnectorProviderSummary[] }) {
  const { t } = useLocale();
  const installed = providers.filter((p) => p.status === 'enabled' || p.status === 'ready' || p.status === 'configured');
  if (installed.length === 0) {
    return <EmptyTab icon={CheckCircle2} title={t('mcp.installed.emptyTitle')} />;
  }
  return (
    <div className="space-y-3">
      {installed.map((provider) => (
        <ConnectorCard key={provider.id} provider={provider} />
      ))}
    </div>
  );
}

function HealthView({ providers }: { providers: ConnectorProviderSummary[] }) {
  const { t } = useLocale();
  return (
    <div className="rounded-lg border bg-card">
      <div className="grid gap-0 divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
        <HealthMetric label={t('mcp.health.providers')} value={providers.length.toString()} />
        <HealthMetric label={t('mcp.health.ready')} value={providers.filter((p) => p.status === 'ready').length.toString()} />
        <HealthMetric label={t('mcp.health.enabled')} value={providers.filter((p) => p.status === 'enabled').length.toString()} />
      </div>
      <div className="border-t p-4 text-sm text-muted-foreground">
        {t('mcp.health.pendingProbe')}
      </div>
    </div>
  );
}

function RawMCPView({
  data,
  error,
  onRetry,
}: {
  data: MCPServerSummary[];
  error: Error | null;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  const [openServers, setOpenServers] = useState<Set<string>>(new Set());

  const toggleServer = useCallback((name: string) => {
    setOpenServers((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  if (error && data.length === 0) {
    return <ErrorCard error={error} title={t('mcp.errorTitle')} onRetry={onRetry} />;
  }

  if (data.length === 0) {
    return (
      <div className="rounded-lg border border-dashed py-12 text-center">
        <div className="flex flex-col items-center gap-2 text-muted-foreground">
          <Plug size={32} className="opacity-40" />
          <p className="text-sm font-medium">{t('mcp.emptyTitle')}</p>
          <p className="max-w-md text-xs" dangerouslySetInnerHTML={{ __html: t('mcp.emptyHint') }} />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {data.map((srv) => (
        <ServerCard
          key={srv.name}
          server={srv}
          isOpen={openServers.has(srv.name)}
          onToggle={() => toggleServer(srv.name)}
        />
      ))}
    </div>
  );
}

function ServerCard({
  server,
  isOpen,
  onToggle,
}: {
  server: MCPServerSummary;
  isOpen: boolean;
  onToggle: () => void;
}) {
  const { t } = useLocale();
  const TransportIcon = server.transport === 'stdio' ? Server : Globe;
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={isOpen}
        className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/30"
      >
        {isOpen ? <ChevronDown size={16} className="shrink-0" /> : <ChevronRight size={16} className="shrink-0" />}
        <TransportIcon size={18} className="shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="font-mono text-sm font-medium">{server.name}</span>
            <span className="text-xs uppercase tracking-wide text-muted-foreground">{server.transport}</span>
          </div>
        </div>
        <span className="text-xs text-muted-foreground">
          {t('mcp.toolCount', { count: server.tool_count })}
        </span>
      </button>
      {isOpen && (
        <div className="divide-y border-t bg-muted/10">
          {server.tools.length === 0 ? (
            <div className="px-12 py-3 text-xs text-muted-foreground">
              {t('mcp.noTools')}
            </div>
          ) : (
            server.tools.map((tool) => (
              <ToolRow key={tool.name} server={server.name} tool={tool} />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ToolRow({ server, tool }: { server: string; tool: MCPToolInfo }) {
  const { t } = useLocale();
  const [showSchema, setShowSchema] = useState(false);
  const [showRun, setShowRun] = useState(false);
  const [args, setArgs] = useState<string>(() => seedArgsFromSchema(tool.input_schema));
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<MCPInvokeResponse | null>(null);
  const [parseErr, setParseErr] = useState<string | null>(null);

  const hasSchema = tool.input_schema && Object.keys(tool.input_schema).length > 0;
  const toolFQN = `mcp_${server}_${tool.name}`;

  const handleRun = useCallback(async () => {
    setParseErr(null);
    let parsed: Record<string, unknown> = {};
    const trimmed = args.trim();
    if (trimmed !== '') {
      try {
        const v = JSON.parse(trimmed);
        if (v === null || typeof v !== 'object' || Array.isArray(v)) {
          setParseErr(t('mcp.argsObjectError'));
          return;
        }
        parsed = v as Record<string, unknown>;
      } catch (e) {
        setParseErr(e instanceof Error ? e.message : 'invalid JSON');
        return;
      }
    }
    setRunning(true);
    setResult(null);
    const toastId = toast.loading(t('mcp.invoking', { tool: toolFQN }));
    try {
      const resp = await api.invokeMCPTool(server, tool.name, parsed);
      setResult(resp);
      if (resp.ok) {
        toast.success(t('mcp.returned', { tool: toolFQN }), { id: toastId });
      } else if (resp.is_error) {
        toast.error(t('mcp.isError'), { id: toastId, description: resp.error?.slice(0, 200) });
      } else {
        toast.error(t('mcp.transportFailed'), { id: toastId, description: resp.error?.slice(0, 200) });
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t('mcp.invokeFailed', { msg }), { id: toastId });
      setResult({ ok: false, error: msg });
    } finally {
      setRunning(false);
    }
  }, [args, server, tool.name, t, toolFQN]);

  return (
    <div className="px-12 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs font-medium">{toolFQN}</span>
        {hasSchema && (
          <button
            type="button"
            onClick={() => setShowSchema((v) => !v)}
            aria-expanded={showSchema}
            className="min-h-[28px] px-1 text-[10px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          >
            {showSchema ? t('mcp.hideSchema') : t('mcp.showSchema')}
          </button>
        )}
        <button
          type="button"
          onClick={() => setShowRun((v) => !v)}
          aria-expanded={showRun}
          className="ml-auto inline-flex min-h-[36px] items-center justify-center gap-1 rounded-md border border-primary/30 bg-primary/5 px-3 py-1.5 text-[11px] text-primary hover:bg-primary/10"
        >
          <Play size={11} />
          {showRun ? t('mcp.hideRun') : t('mcp.run')}
        </button>
      </div>
      {tool.description && (
        <p className="mt-1 text-xs text-muted-foreground">{tool.description}</p>
      )}
      {showSchema && hasSchema && (
        <pre className="mt-2 overflow-x-auto rounded-md border bg-background p-2 font-mono text-[10px] leading-relaxed">
          {JSON.stringify(tool.input_schema, null, 2)}
        </pre>
      )}
      {showRun && (
        <div className="mt-2 space-y-2 rounded-md border bg-muted/10 p-3">
          <label
            className="text-[10px] uppercase tracking-wide text-muted-foreground"
            htmlFor={`mcp-args-${server}-${tool.name}`}
          >
            {t('mcp.argsLabel')}
          </label>
          <textarea
            id={`mcp-args-${server}-${tool.name}`}
            value={args}
            onChange={(e) => setArgs(e.target.value)}
            spellCheck={false}
            rows={Math.min(10, Math.max(3, args.split('\n').length))}
            className="min-h-24 w-full rounded-md border bg-background p-2 font-mono text-[11px] focus:outline-none focus:ring-2 focus:ring-primary/30"
          />
          {parseErr && (
            <div className="flex items-center gap-1.5 text-[11px] text-destructive">
              <AlertTriangle size={12} />
              {parseErr}
            </div>
          )}
          <div className="flex justify-end">
            <button
              type="button"
              onClick={() => void handleRun()}
              disabled={running}
              className="inline-flex min-h-11 items-center justify-center gap-1.5 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {running ? <Loader2 size={12} className="animate-spin" /> : <Play size={12} />}
              {running ? t('mcp.calling') : t('mcp.invoke')}
            </button>
          </div>
          {result && <ToolResult result={result} />}
        </div>
      )}
    </div>
  );
}

function ToolResult({ result }: { result: MCPInvokeResponse }) {
  const { t } = useLocale();
  const tone =
    result.ok ? 'border-emerald-500/30 bg-emerald-500/5' :
    result.is_error ? 'border-amber-500/30 bg-amber-500/5' :
    'border-destructive/30 bg-destructive/5';
  const Icon = result.ok ? CheckCircle2 : AlertTriangle;
  const iconColor = result.ok ? 'text-emerald-600 dark:text-emerald-400' : result.is_error ? 'text-amber-600 dark:text-amber-400' : 'text-destructive';
  const title = result.ok ? t('mcp.success') : result.is_error ? t('mcp.isErrorTitle') : t('mcp.transportTimeout');
  const body = result.ok ? result.output : (result.error || result.output || t('mcp.noDetail'));
  return (
    <div className={`rounded-md border ${tone} p-2`}>
      <div className={`flex items-center gap-1.5 text-[11px] font-medium ${iconColor}`}>
        <Icon size={12} />
        {title}
      </div>
      {body && (
        <pre className="mt-1.5 max-h-64 overflow-y-auto whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-muted-foreground">
          {body}
        </pre>
      )}
    </div>
  );
}

function StatusPill({ status }: { status: ConnectorProviderSummary['status'] }) {
  const { t } = useLocale();
  const cls =
    status === 'enabled' ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' :
    status === 'ready' ? 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300' :
    status === 'failed' ? 'border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300' :
    'border-border bg-muted/40 text-muted-foreground';
  return (
    <span className={`rounded-md border px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${cls}`}>
      {t(`mcp.providers.status.${status}`)}
    </span>
  );
}

function MetaPill({ label }: { label: string }) {
  return (
    <span className="rounded-md border bg-muted/30 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
      {label}
    </span>
  );
}

function RiskPill({ badge }: { badge: ConnectorRiskBadge }) {
  const cls =
    badge.level === 'high' ? 'border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300' :
    badge.level === 'medium' ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300' :
    'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${cls}`}>
      <ShieldAlert size={11} />
      {badge.label}
    </span>
  );
}

function ToolList({ values, tone }: { values: string[]; tone: 'approved' | 'blocked' }) {
  const { t } = useLocale();
  if (values.length === 0) return null;
  const cls = tone === 'approved'
    ? 'border-emerald-500/20 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300'
    : 'border-rose-500/20 bg-rose-500/5 text-rose-700 dark:text-rose-300';
  return (
    <div className="space-y-1">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {tone === 'approved' ? t('mcp.providers.approvedTools') : t('mcp.providers.blockedTools')}
      </div>
      <div className="flex flex-wrap gap-1">
        {values.slice(0, 8).map((value) => (
          <code key={value} className={`rounded border px-1.5 py-0.5 text-[10px] ${cls}`}>
            {value}
          </code>
        ))}
        {values.length > 8 && (
          <span className="text-[10px] text-muted-foreground">+{values.length - 8}</span>
        )}
      </div>
    </div>
  );
}

function HealthMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function EmptyTab({ icon: Icon, title }: { icon: LucideIcon; title: string }) {
  return (
    <div className="rounded-lg border border-dashed py-12 text-center">
      <div className="flex flex-col items-center gap-2 text-muted-foreground">
        <Icon size={32} className="opacity-40" />
        <p className="text-sm font-medium">{title}</p>
      </div>
    </div>
  );
}

function seedArgsFromSchema(schema: Record<string, unknown> | undefined): string {
  if (!schema) return '{}';
  const props = schema.properties as Record<string, { type?: string }> | undefined;
  if (!props || Object.keys(props).length === 0) return '{}';
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(props)) {
    switch (v?.type) {
      case 'integer':
      case 'number':
        out[k] = 0;
        break;
      case 'boolean':
        out[k] = false;
        break;
      case 'array':
        out[k] = [];
        break;
      case 'object':
        out[k] = {};
        break;
      default:
        out[k] = '';
    }
  }
  return JSON.stringify(out, null, 2);
}

function MCPSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-4 w-32" />
      </div>
      <Skeleton className="h-9 w-96 max-w-full" />
      <div className="grid gap-3 xl:grid-cols-2">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-64 w-full rounded-lg" />
        ))}
      </div>
    </div>
  );
}

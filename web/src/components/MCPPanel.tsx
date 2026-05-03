import { useCallback, useState } from 'react';
import { Plug, ChevronDown, ChevronRight, Server, Globe, Play, Loader2, AlertTriangle, CheckCircle2 } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import type { MCPServerSummary, MCPToolInfo, MCPInvokeResponse } from '@/types/api';

// MCPPanel surfaces every MCP server Aura connected to at boot, the
// tools they advertise, and (slice 11d) lets the operator invoke each
// tool with a JSON argument body straight from the dashboard.
export function MCPPanel() {
  const { t } = useLocale();
  const fetcher = useCallback(() => api.mcpServers(), []);
  const { data, error, loading, refetch } = useApi(fetcher);
  const [openServers, setOpenServers] = useState<Set<string>>(new Set());

  const toggleServer = useCallback((name: string) => {
    setOpenServers((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  if (loading && !data) return <MCPSkeleton />;
  if (error && !data) {
    return <ErrorCard error={error} title={t('mcp.errorTitle')} onRetry={refetch} />;
  }
  if (!data) return null;

  const totalTools = data.reduce((acc, s) => acc + s.tool_count, 0);

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t('mcp.title')}</h1>
          <p className="text-xs text-muted-foreground mt-1" dangerouslySetInnerHTML={{ __html: t('mcp.subtitle') }} />
        </div>
        <button
          type="button"
          onClick={refetch}
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          {t('mcp.serverCount', { count: data.length })} · {t('mcp.toolCount', { count: totalTools })}
        </button>
      </header>

      {data.length === 0 ? (
        <div className="rounded-lg border border-dashed py-12 text-center">
          <div className="flex flex-col items-center gap-2 text-muted-foreground">
            <Plug size={32} className="opacity-40" />
            <p className="text-sm font-medium">{t('mcp.emptyTitle')}</p>
            <p className="text-xs max-w-md mx-auto" dangerouslySetInnerHTML={{ __html: t('mcp.emptyHint') }} />
          </div>
        </div>
      ) : (
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
      )}
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
    <div className="rounded-lg border bg-card overflow-hidden">
      <button
        type="button"
        onClick={onToggle}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted/30"
      >
        {isOpen ? <ChevronDown size={16} className="shrink-0" /> : <ChevronRight size={16} className="shrink-0" />}
        <TransportIcon size={18} className="text-primary shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="font-mono text-sm font-medium">{server.name}</span>
            <span className="text-xs text-muted-foreground uppercase tracking-wide">{server.transport}</span>
          </div>
        </div>
        <span className="text-xs text-muted-foreground">
          {t('mcp.toolCount', { count: server.tool_count })}
        </span>
      </button>
      {isOpen && (
        <div className="border-t bg-muted/10 divide-y">
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
      <div className="flex items-center gap-2 flex-wrap">
        <span className="font-mono text-xs font-medium">{toolFQN}</span>
        {hasSchema && (
          <button
            type="button"
            onClick={() => setShowSchema((v) => !v)}
            className="text-[10px] text-muted-foreground hover:text-foreground underline-offset-2 hover:underline min-h-[28px] px-1"
          >
            {showSchema ? t('mcp.hideSchema') : t('mcp.showSchema')}
          </button>
        )}
        <button
          type="button"
          onClick={() => setShowRun((v) => !v)}
          className="ml-auto inline-flex items-center justify-center gap-1 rounded-md border border-primary/30 bg-primary/5 px-3 py-1.5 text-[11px] text-primary hover:bg-primary/10 min-h-[36px]"
        >
          <Play size={11} />
          {showRun ? t('mcp.hideRun') : t('mcp.run')}
        </button>
      </div>
      {tool.description && (
        <p className="mt-1 text-xs text-muted-foreground">{tool.description}</p>
      )}
      {showSchema && hasSchema && (
        <pre className="mt-2 rounded-md border bg-background p-2 text-[10px] font-mono leading-relaxed overflow-x-auto">
          {JSON.stringify(tool.input_schema, null, 2)}
        </pre>
      )}
      {showRun && (
        <div className="mt-2 space-y-2 rounded-md border bg-muted/10 p-3">
          <label className="text-[10px] uppercase tracking-wide text-muted-foreground">{t('mcp.argsLabel')}</label>
          <textarea
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
        <pre className="mt-1.5 whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-muted-foreground max-h-64 overflow-y-auto">
          {body}
        </pre>
      )}
    </div>
  );
}

// seedArgsFromSchema produces a starter JSON body for the textarea so
// the operator doesn't have to type out every property name. Reads
// inputSchema.properties and emits zero-values per declared type.
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
      {[0, 1].map((i) => (
        <div key={i} className="rounded-lg border overflow-hidden">
          <div className="px-4 py-3 flex items-center gap-3">
            <Skeleton className="h-4 w-4" />
            <Skeleton className="h-5 w-5" />
            <Skeleton className="h-4 w-32 flex-1" />
            <Skeleton className="h-3 w-16" />
          </div>
        </div>
      ))}
    </div>
  );
}

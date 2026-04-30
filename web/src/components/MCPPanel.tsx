import { useCallback, useState } from 'react';
import { Plug, ChevronDown, ChevronRight, Server, Globe } from 'lucide-react';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { Skeleton } from '@/components/ui/skeleton';
import type { MCPServerSummary, MCPToolInfo } from '@/types/api';

// MCPPanel surfaces every MCP (Model Context Protocol) server Aura
// connected to at boot and the tools each one advertised. Read-only —
// invocation from the dashboard arrives in slice 11d.
export function MCPPanel() {
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
    return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
  }
  if (!data) return null;

  const totalTools = data.reduce((acc, s) => acc + s.tool_count, 0);

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">MCP Servers</h1>
          <p className="text-xs text-muted-foreground mt-1">
            Tools advertised by external MCP servers, mounted as <code className="font-mono">mcp_&lt;server&gt;_&lt;tool&gt;</code>. Configure in <code className="font-mono">mcp.json</code>.
          </p>
        </div>
        <button
          type="button"
          onClick={refetch}
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          {data.length} server{data.length !== 1 ? 's' : ''} · {totalTools} tool{totalTools !== 1 ? 's' : ''}
        </button>
      </header>

      {data.length === 0 ? (
        <div className="rounded-lg border border-dashed py-12 text-center">
          <div className="flex flex-col items-center gap-2 text-muted-foreground">
            <Plug size={32} className="opacity-40" />
            <p className="text-sm font-medium">No MCP servers connected</p>
            <p className="text-xs max-w-md mx-auto">
              Copy <code className="font-mono">mcp.example.json</code> to <code className="font-mono">mcp.json</code> at the repo root and restart Aura. Each entry needs either <code className="font-mono">command</code>+<code className="font-mono">args</code> (stdio) or <code className="font-mono">url</code>+<code className="font-mono">headers</code> (HTTP).
            </p>
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
          {server.tool_count} tool{server.tool_count !== 1 ? 's' : ''}
        </span>
      </button>
      {isOpen && (
        <div className="border-t bg-muted/10 divide-y">
          {server.tools.length === 0 ? (
            <div className="px-12 py-3 text-xs text-muted-foreground">
              No tools advertised by this server.
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
  const [expanded, setExpanded] = useState(false);
  const hasSchema = tool.input_schema && Object.keys(tool.input_schema).length > 0;
  return (
    <div className="px-12 py-2.5">
      <div className="flex items-baseline gap-2">
        <span className="font-mono text-xs font-medium">mcp_{server}_{tool.name}</span>
        {hasSchema && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-[10px] text-muted-foreground hover:text-foreground underline-offset-2 hover:underline"
          >
            {expanded ? 'hide schema' : 'show schema'}
          </button>
        )}
      </div>
      {tool.description && (
        <p className="mt-1 text-xs text-muted-foreground">{tool.description}</p>
      )}
      {expanded && hasSchema && (
        <pre className="mt-2 rounded-md border bg-background p-2 text-[10px] font-mono leading-relaxed overflow-x-auto">
          {JSON.stringify(tool.input_schema, null, 2)}
        </pre>
      )}
    </div>
  );
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

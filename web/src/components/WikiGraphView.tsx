import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ExternalLink, Maximize2, Search } from 'lucide-react';
import ForceGraph2D from 'react-force-graph-2d';
import type { ForceGraphMethods } from 'react-force-graph-2d';
import { useNavigate } from 'react-router-dom';
import { api } from '@/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { GraphNode, GraphEdge } from '@/types/api';

interface ForceNode extends GraphNode {
  // react-force-graph mutates nodes with x/y/vx/vy at runtime
  x?: number; y?: number; vx?: number; vy?: number;
}
interface ForceLink {
  source: string | ForceNode;
  target: string | ForceNode;
  type: GraphEdge['type'];
}

const CATEGORY_COLORS: Record<string, string> = {
  notes: '#60a5fa',
  sources: '#34d399',
  ideas: '#f472b6',
  default: '#94a3b8',
};

function colorFor(category: string | undefined): string {
  return CATEGORY_COLORS[category ?? ''] ?? CATEGORY_COLORS.default;
}

export default function WikiGraphView() {
  const { t } = useLocale();
  const fetcher = useCallback(() => api.wikiGraph(), []);
  const { data, loading, error } = useApi(fetcher);
  const navigate = useNavigate();
  const containerRef = useRef<HTMLDivElement>(null);
  const graphRef = useRef<ForceGraphMethods<ForceNode, ForceLink> | undefined>(undefined);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [query, setQuery] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const normalizedQuery = query.trim().toLowerCase();
  const nodes = useMemo(() => (Array.isArray(data?.nodes) ? data.nodes : []), [data]);
  const edges = useMemo(() => (Array.isArray(data?.edges) ? data.edges : []), [data]);
  const matchedIds = useMemo(() => {
    if (!normalizedQuery) return new Set<string>();
    return new Set(nodes
      .filter((n) => [n.title, n.id, n.category].some((value) => value?.toLowerCase().includes(normalizedQuery)))
      .map((n) => n.id));
  }, [nodes, normalizedQuery]);
  const selectedNode = nodes.find((n) => n.id === selectedId);

  useEffect(() => {
    if (!containerRef.current) return;
    const measure = () => {
      if (!containerRef.current) return;
      const r = containerRef.current.getBoundingClientRect();
      setSize({
        width: Math.max(0, Math.floor(r.width)),
        height: Math.max(360, Math.floor(r.height)),
      });
    };
    measure();
    const obs = new ResizeObserver((entries) => {
      const r = entries[0].contentRect;
      setSize({
        width: Math.max(0, Math.floor(r.width)),
        height: Math.max(360, Math.floor(r.height)),
      });
    });
    obs.observe(containerRef.current);
    const raf = requestAnimationFrame(measure);
    return () => {
      cancelAnimationFrame(raf);
      obs.disconnect();
    };
  }, [data]);

  if (loading && !data) return <div className="p-6 text-sm text-muted-foreground">{t('graph.loading')}</div>;
  if (error && !data) return <div className="p-6 text-sm text-destructive">{t('graph.error', { message: error.message })}</div>;
  if (!data) return null;

  const matchCount = normalizedQuery ? matchedIds.size : nodes.length;

  const graphData = {
    nodes: nodes.map((n) => ({ ...n })) as ForceNode[],
    links: edges.map((e) => ({ source: e.source, target: e.target, type: e.type })) as ForceLink[],
  };
  const fitGraph = () => {
    graphRef.current?.zoomToFit(450, 48, (node) => {
      if (!normalizedQuery) return true;
      const id = typeof node.id === 'string' ? node.id : String(node.id ?? '');
      return matchedIds.has(id);
    });
  };

  return (
    <div className="flex h-full min-h-[420px] flex-col">
      <h1 className="sr-only">{t('graph.title')}</h1>
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <label className="relative min-w-[220px] flex-1 md:max-w-sm">
          <span className="sr-only">{t('graph.searchLabel')}</span>
          <Search className="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('graph.searchPlaceholder')}
            className="pl-8"
          />
        </label>
        <Button type="button" variant="outline" onClick={fitGraph} title={t('graph.fitHint')}>
          <Maximize2 />
          {t('graph.fit')}
        </Button>
        <div className="ml-auto text-sm text-muted-foreground" aria-live="polite">
          {t('graph.counts', { nodes: nodes.length, edges: edges.length })}
          {normalizedQuery && <span className="ml-2">{t('graph.matches', { count: matchCount })}</span>}
        </div>
      </div>
      {selectedNode && (
        <div className="flex flex-wrap items-center gap-2 border-b bg-muted/30 px-4 py-2 text-sm">
          <span className="font-medium">{selectedNode.title}</span>
          {selectedNode.category && <span className="rounded-md bg-background px-2 py-1 text-xs text-muted-foreground">{selectedNode.category}</span>}
          <Button type="button" variant="ghost" size="sm" onClick={() => navigate(`/wiki/${selectedNode.id}`)}>
            <ExternalLink />
            {t('graph.openPage')}
          </Button>
        </div>
      )}
      {nodes.length === 0 && (
        <div className="p-6 text-sm text-muted-foreground">{t('graph.empty')}</div>
      )}
      <div ref={containerRef} className="min-h-[360px] flex-1 overflow-hidden">
      {size.width > 0 && size.height > 0 ? (
        <ForceGraph2D
          ref={graphRef}
          graphData={graphData}
          width={size.width}
          height={size.height}
          nodeRelSize={6}
          nodeVal={(n: ForceNode) => (n.id === selectedId ? 2.2 : normalizedQuery && matchedIds.has(n.id) ? 1.7 : 1)}
          nodeColor={(n: ForceNode) => {
            if (n.id === selectedId) return '#f59e0b';
            if (normalizedQuery && !matchedIds.has(n.id)) return '#cbd5e1';
            return colorFor(n.category);
          }}
          nodeLabel={(n: ForceNode) => `${n.title}${n.category ? ` (${n.category})` : ''}`}
          linkColor={(l: ForceLink) => (l.type === 'wikilink' ? '#a3a3a3' : '#d4a3a3')}
          linkWidth={(l: ForceLink) => (selectedId && (linkEndpointId(l.source) === selectedId || linkEndpointId(l.target) === selectedId) ? 2 : 1)}
          onNodeClick={(n) => setSelectedId((n as ForceNode).id)}
          cooldownTicks={100}
        />
      ) : (
        <div className="flex h-[360px] items-center justify-center text-sm text-muted-foreground">
          {t('graph.measuring')}
        </div>
      )}
      </div>
      {nodes.length > 0 && (
        <div className="border-t p-3 md:hidden">
          <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">{t('graph.nodes')}</p>
          <div className="space-y-1">
            {nodes.slice(0, 12).map((node) => (
              <button
                key={node.id}
                type="button"
                onClick={() => navigate(`/wiki/${node.id}`)}
                className="flex min-h-11 w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm hover:bg-muted"
              >
                <span className="min-w-0 truncate">{node.title}</span>
                {node.category && <span className="ml-3 shrink-0 text-xs text-muted-foreground">{node.category}</span>}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function linkEndpointId(endpoint: ForceLink['source'] | ForceLink['target']): string {
  if (typeof endpoint === 'string') return endpoint;
  if (typeof endpoint === 'object' && endpoint && 'id' in endpoint) return String(endpoint.id ?? '');
  return '';
}

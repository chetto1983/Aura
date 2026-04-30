import { useCallback, useEffect, useRef, useState } from 'react';
import ForceGraph2D from 'react-force-graph-2d';
import { useNavigate } from 'react-router-dom';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { GraphNode, GraphEdge } from '@/types/api';

interface ForceNode extends GraphNode {
  // react-force-graph mutates nodes with x/y/vx/vy at runtime
  x?: number; y?: number; vx?: number; vy?: number;
}
interface ForceLink {
  source: string;
  target: string;
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
  const fetcher = useCallback(() => api.wikiGraph(), []);
  const { data, loading, error } = useApi(fetcher);
  const navigate = useNavigate();
  const containerRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 800, height: 600 });

  useEffect(() => {
    if (!containerRef.current) return;
    const obs = new ResizeObserver((entries) => {
      const r = entries[0].contentRect;
      setSize({ width: r.width, height: r.height });
    });
    obs.observe(containerRef.current);
    return () => obs.disconnect();
  }, []);

  if (loading && !data) return <div className="p-6 text-sm text-muted-foreground">Loading graph…</div>;
  if (error && !data) return <div className="p-6 text-sm text-destructive">Error: {error.message}</div>;
  if (!data) return null;

  const graphData = {
    nodes: data.nodes.map((n) => ({ ...n })) as ForceNode[],
    links: data.edges.map((e) => ({ source: e.source, target: e.target, type: e.type })) as ForceLink[],
  };

  return (
    <div ref={containerRef} className="h-full w-full">
      <ForceGraph2D
        graphData={graphData}
        width={size.width}
        height={size.height}
        nodeRelSize={6}
        nodeColor={(n: ForceNode) => colorFor(n.category)}
        nodeLabel={(n: ForceNode) => `${n.title}${n.category ? ` (${n.category})` : ''}`}
        linkColor={(l: ForceLink) => (l.type === 'wikilink' ? '#a3a3a3' : '#d4a3a3')}
        linkWidth={1}
        onNodeClick={(n) => navigate(`/wiki/${(n as ForceNode).id}`)}
        cooldownTicks={100}
      />
    </div>
  );
}

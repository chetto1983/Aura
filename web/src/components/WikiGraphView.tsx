/** @ts-nocheck */
/* eslint-disable @typescript-eslint/no-explicit-any, react-hooks/set-state-in-effect */
/**
 * WikiGraphView
 * ----------------------------------------------------------------------------
 * Interactive force-directed graph of the Sacchi wiki. Clicking a category
 * node loads its products into a side panel (via ``max_category_products``
 * backend parameter on re-fetch) and exposes a ``onSelectProduct`` callback.
 *
 * Deps:
 *   npm install react-force-graph-2d
 *
 * API contract (backend): GET /api/tools/wiki/graph
 *   ?include_categories=true&max_category_products=0
 *   -> { nodes: [{id, name, type, path, val, color, sacchi_code?, description?}],
 *        links: [{source, target}],
 *        legend: {type: color, ...} }
 */
import { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import ForceGraph2D from 'react-force-graph-2d';
import { GraphSkeleton, Skeleton } from './common/AppSkeletons';
import { MarkdownReader } from './common/MarkdownReader';
import { ProductDetailPanel } from './ProductDetailPanel';
import WikiEditor from './WikiEditor';

type GraphNode = {
  id: string;
  name: string;
  type: string;
  path?: string;
  val: number;
  color: string;
  sacchi_code?: string;
  description?: string;
};

type GraphLink = { source: string; target: string };

type GraphPayload = {
  node_count: number;
  link_count: number;
  nodes: GraphNode[];
  links: GraphLink[];
  legend: Record<string, string>;
};

type GraphPalette = {
  canvas: string;
  label: string;
  link: string;
};

function readGraphPalette(): GraphPalette {
  if (typeof window === 'undefined') {
    return { canvas: '#ffffff', label: 'rgba(17,24,39,0.85)', link: 'rgba(15,23,42,0.18)' };
  }
  const root = document.documentElement;
  const styles = window.getComputedStyle(root);
  const theme = root.dataset.theme;
  return {
    canvas: styles.getPropertyValue('--bg').trim() || '#ffffff',
    label: theme === 'dark' ? 'rgba(226,232,240,0.86)' : 'rgba(17,24,39,0.85)',
    link: theme === 'dark' ? 'rgba(148,163,184,0.28)' : 'rgba(15,23,42,0.18)',
  };
}

export function WikiGraphView({
  onClose,
  onSelectProduct,
  onSelectWikiPage,
}: {
  onClose: () => void;
  onSelectProduct?: (sacchiCode: string) => void;
  onSelectWikiPage?: (path: string) => void;
}) {
  const fgRef = useRef<any>(null);
  const [data, setData] = useState<GraphPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [typeFilter, setTypeFilter] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState('');
  const [sampleProducts, setSampleProducts] = useState(0);
  const [selected, setSelected] = useState<GraphNode | null>(null);
  const [pageBody, setPageBody] = useState<string>('');
  const [pageLoading, setPageLoading] = useState(false);
  const [pageError, setPageError] = useState<string | null>(null);
  const [palette, setPalette] = useState<GraphPalette>(readGraphPalette);
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    setPalette(readGraphPalette());
    const observer = new MutationObserver(() => setPalette(readGraphPalette()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    return () => observer.disconnect();
  }, []);

  // Fetch the markdown body when a wiki node (non-product) is selected.
  useEffect(() => {
    setPageBody('');
    setPageError(null);
    if (!selected || selected.type === 'product' || !selected.path) return;
    const ref = selected.path;
    setPageLoading(true);
    fetch(`/api/tools/wiki/page?page_ref=${encodeURIComponent(ref)}&max_chars=20000`)
      .then((r) => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.json(); })
      .then((p: { content?: string; title?: string }) => {
        setPageBody(typeof p?.content === 'string' ? p.content : '');
        setPageLoading(false);
      })
      .catch((err) => { setPageError(String(err)); setPageLoading(false); });
  }, [selected]);

  const fetchGraph = useCallback((samplePerCategory: number) => {
    setLoading(true);
    setError(null);
    const url = `/api/tools/wiki/graph?include_categories=true&max_category_products=${samplePerCategory}`;
    fetch(url)
      .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.json(); })
      .then((payload: GraphPayload) => { setData(payload); setLoading(false); })
      .catch(err => { setError(String(err)); setLoading(false); });
  }, []);

  useEffect(() => { fetchGraph(sampleProducts); }, [fetchGraph, sampleProducts]);

  // Escape closes the modal — without this, modals trap the keystroke and any
  // global Escape handler downstream (sidebar mobile drawer, dialogs that need
  // to be reset before re-opening) silently no-ops. UX antipattern fix.
  // Guard: skip when a child dialog is open (e.g. ProductCard image lightbox)
  // so ESC dismisses only the topmost overlay, not the graph view too.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      // Skip when a child overlay handled the keystroke (Radix Dialog calls
      // preventDefault on ESC) so the graph stays open while the lightbox closes.
      if (e.defaultPrevented) return;
      if (document.querySelector('[role="dialog"]')) return;
      onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const filteredData = useMemo(() => {
    if (!data) return null;
    const searchLower = search.trim().toLowerCase();
    const visibleNodes = data.nodes.filter(n => {
      if (typeFilter.size > 0 && !typeFilter.has(n.type)) return false;
      if (searchLower && !n.name.toLowerCase().includes(searchLower) && !n.id.toLowerCase().includes(searchLower)) return false;
      return true;
    });
    const visibleIds = new Set(visibleNodes.map(n => n.id));
    const visibleLinks = data.links.filter(l => visibleIds.has(typeof l.source === 'string' ? l.source : (l.source as any).id) && visibleIds.has(typeof l.target === 'string' ? l.target : (l.target as any).id));
    return { nodes: visibleNodes, links: visibleLinks, legend: data.legend, node_count: visibleNodes.length, link_count: visibleLinks.length };
  }, [data, typeFilter, search]);

  const allTypes = useMemo(() => {
    if (!data) return [] as string[];
    const s = new Set<string>();
    data.nodes.forEach(n => s.add(n.type));
    return Array.from(s).sort();
  }, [data]);

  const handleNodeClick = useCallback((node: GraphNode) => {
    setSelected(node);
    const n = node as any;
    if (fgRef.current?.centerAt) {
      fgRef.current.centerAt(n.x, n.y, 600);
      fgRef.current.zoom(3, 600);
    }
    if (node.type === 'product' && node.sacchi_code && onSelectProduct) {
      onSelectProduct(node.sacchi_code);
    } else if (node.path && onSelectWikiPage) {
      onSelectWikiPage(node.path);
    }
  }, [onSelectProduct, onSelectWikiPage]);

  const toggleType = (t: string) => {
    setTypeFilter(prev => {
      const next = new Set(prev);
      if (next.has(t)) next.delete(t); else next.add(t);
      return next;
    });
  };

  return (
    <div className="sacchi-graph-page" data-testid="wiki-graph-view">
      <div className="sacchi-graph-page__header">
        <div className="sacchi-graph-page__title-group">
          <h2 className="sacchi-graph-page__title">Grafo wiki</h2>
          <span className="sacchi-graph-page__count">
            {filteredData ? `${filteredData.node_count} nodi · ${filteredData.link_count} link` : '—'}
          </span>
        </div>
        <button type="button" onClick={onClose} className="sacchi-graph-page__close">Torna alla chat</button>
      </div>

      <div className="sacchi-graph-page__controls">
        <input
          type="text"
          placeholder="Cerca nel grafo…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="sacchi-graph-page__search"
        />
        <div className="sacchi-graph-page__type-filter">
          <span className="sacchi-graph-page__filter-label">Tipi:</span>
          {allTypes.map(t => {
            const active = typeFilter.size === 0 || typeFilter.has(t);
            const typeColor = data?.legend[t];
            return (
              <button
                key={t}
                type="button"
                onClick={() => toggleType(t)}
                className={`sacchi-graph-page__type-btn${active ? ' sacchi-graph-page__type-btn--active' : ''}`}
                style={typeColor ? ({ ['--graph-type-color' as any]: typeColor }) : undefined}
              >{t}</button>
            );
          })}
        </div>
        <label className="sacchi-graph-page__sample-control">
          Prodotti per categoria:
          <select
            value={sampleProducts}
            onChange={(e) => setSampleProducts(Number(e.target.value))}
            className="sacchi-graph-page__sample-select"
            data-testid="wiki-graph-sample-products"
          >
            <option value={0}>0 (solo wiki)</option>
            <option value={3}>3</option>
            <option value={5}>5</option>
            <option value={10}>10</option>
          </select>
        </label>
      </div>

      <div className="sacchi-graph-page__body">
        <div className="sacchi-graph-page__canvas">
          {loading && <GraphSkeleton />}
          {error && <div className="sacchi-graph-page__error">Errore: {error}</div>}
          {!loading && !error && filteredData && (
            <ForceGraph2D
              ref={fgRef}
              graphData={filteredData}
              nodeLabel={(n: any) => `${n.name}${n.type === 'product' ? ` · ${n.description || ''}` : ''}`}
              nodeColor={(n: any) => n.color}
              nodeVal={(n: any) => n.val}
              linkColor={() => palette.link}
              linkWidth={1}
              onNodeClick={handleNodeClick}
              cooldownTicks={100}
              d3AlphaDecay={0.03}
              backgroundColor={palette.canvas}
              nodeCanvasObjectMode={() => 'after'}
              nodeCanvasObject={(node: any, ctx: CanvasRenderingContext2D, globalScale: number) => {
                if (globalScale < 1.5 && node.type !== 'category') return;
                const label = node.name as string;
                const fontSize = Math.max(10 / globalScale, 6);
                ctx.font = `${fontSize}px Inter, sans-serif`;
                ctx.fillStyle = palette.label;
                ctx.textAlign = 'center';
                ctx.textBaseline = 'middle';
                ctx.fillText(label.length > 30 ? label.slice(0, 30) + '…' : label, node.x, (node.y as number) + (node.val || 4) + fontSize);
              }}
            />
          )}
          {/* Blocker 3: programmatic per-product-node affordance.
              react-force-graph-2d renders every node to a <canvas>, so Playwright cannot
              click a specific node by name. This list mirrors the canvas state — clicking
              a button calls setSelected(node), the SAME setter handleNodeClick uses, so
              the user-visible behavior is identical (panel opens, graph stays open per D-21).
              Hidden behind a small <details> toggle to keep the canvas the primary surface;
              always present in the DOM so smoke battery scenarios can drive it. */}
          {filteredData && filteredData.nodes.some((n: any) => n.type === 'product') && (
            <details
              className="sacchi-graph__product-list"
              data-testid="wiki-graph-product-list"
            >
              <summary className="sacchi-graph__product-list-summary">
                Prodotti ({filteredData.nodes.filter((n: any) => n.type === 'product').length})
              </summary>
              <div className="sacchi-graph__product-list-items">
                {filteredData.nodes
                  .filter((n: any) => n.type === 'product')
                  .map((n: any) => {
                    const code = n.sacchi_code || n.id || '';
                    return (
                      <button
                        key={n.id}
                        type="button"
                        data-testid={`graph-node-list-item-product-${code}`}
                        onClick={() => setSelected(n)}
                        className="sacchi-graph__product-list-item"
                      >
                        {n.name || code}
                      </button>
                    );
                  })}
              </div>
            </details>
          )}
        </div>
        {selected && (
          <aside className="sacchi-graph-detail">
            <div className="sacchi-graph-detail__header">
              <h3 className="sacchi-graph-detail__title">{selected.name}</h3>
              <button
                type="button"
                onClick={() => setSelected(null)}
                aria-label="Chiudi pannello"
                className="sacchi-graph-detail__close"
              >
                <span aria-hidden="true">×</span>
              </button>
            </div>
            <div className="sacchi-graph-detail__type-row">
              <span
                className="sacchi-graph-detail__type-badge"
                style={{ ['--graph-type-color' as any]: selected.color }}
              >{selected.type}</span>
            </div>
            {selected.path && (
              <div className="sacchi-graph-detail__path-row flex items-center justify-between gap-2">
                <span className="sacchi-graph-detail__path">{selected.path}</span>
                {selected.type !== 'product' && (
                  <button
                    type="button"
                    onClick={() => setEditing(true)}
                    className="rounded border border-emerald-600 bg-white px-2 py-0.5 text-xs font-medium text-emerald-700 hover:bg-emerald-50"
                    aria-label="Modifica pagina wiki"
                    title="Apri editor full-screen (Ctrl+S per salvare)"
                  >Modifica</button>
                )}
              </div>
            )}
            {selected.description && (
              <div className="sacchi-graph-detail__description">{selected.description}</div>
            )}
            {selected.type === 'product' && selected.sacchi_code && (
              <ProductDetailPanel
                sacchiCode={selected.sacchi_code}
                onAskAgent={(code) => onSelectProduct?.(code)}
              />
            )}
            {selected.type === 'category' && (
              <button
                type="button"
                onClick={() => setSampleProducts(prev => prev === 0 ? 5 : prev)}
                className="sacchi-graph-detail__cta"
              >Mostra prodotti di questa categoria</button>
            )}
            {selected.type !== 'product' && selected.path && (
              <div data-testid="wiki-page-body" className="sacchi-graph-detail__page">
                {pageLoading && (
                  <div className="space-y-2" role="status" aria-label="Caricamento pagina wiki">
                    <Skeleton className="h-3 w-3/4" />
                    <Skeleton className="h-3 w-full" />
                    <Skeleton className="h-3 w-5/6" />
                  </div>
                )}
                {pageError && <div className="sacchi-graph-detail__page-error">Errore: {pageError}</div>}
                {!pageLoading && !pageError && pageBody && (
                  <MarkdownReader markdown={pageBody} />
                )}
                {!pageLoading && !pageError && !pageBody && (
                  <div className="sacchi-graph-detail__page-empty">
                    File Markdown non trovato o non sincronizzato.
                  </div>
                )}
              </div>
            )}
          </aside>
        )}
      </div>
      {editing && selected?.path && pageBody && (
        <WikiEditor
          pagePath={selected.path}
          initialMarkdown={pageBody}
          onClose={() => setEditing(false)}
          onSaved={(newBody) => setPageBody(newBody)}
        />
      )}
    </div>
  );
}

export default WikiGraphView;

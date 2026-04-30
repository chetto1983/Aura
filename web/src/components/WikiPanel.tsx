/** @ts-nocheck */
import { useState, useEffect } from 'react';

interface Node {
  id: string;
  title: string;
  path: string;
  type: string;
}

export function WikiPanel({ onClose }: { onClose: () => void }) {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/tools/wiki/pages')
      .then(r => r.json())
      .then((data: Node[]) => { setNodes(Array.isArray(data) ? data : []); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const filtered = nodes.filter(n =>
    n.title.toLowerCase().includes(filter.toLowerCase()) ||
    n.id.toLowerCase().includes(filter.toLowerCase())
  );

  return (
    <div className="sacchi-wiki-panel">
      <div className="sacchi-wiki-panel__header">
        <h3 className="sacchi-wiki-panel__title">Wiki Explorer</h3>
        <button
          type="button"
          onClick={onClose}
          aria-label="Chiudi Wiki Explorer"
          className="sacchi-wiki-panel__close"
        >
          <span aria-hidden="true">✕</span>
        </button>
      </div>
      <input
        type="search"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Cerca pagina..."
        aria-label="Cerca pagina wiki"
        className="sacchi-wiki-panel__search"
      />
      <div className="sacchi-wiki-panel__list">
        {loading && <div className="sacchi-wiki-panel__loading">Caricamento...</div>}
        {filtered.map(n => (
          <a
            key={n.id}
            href={`/api/tools/wiki/page?id=${encodeURIComponent(n.id)}`}
            target="_blank"
            rel="noreferrer"
            className="sacchi-wiki-panel__item"
          >
            <div className="sacchi-wiki-panel__item-title">{n.title}</div>
            <div className="sacchi-wiki-panel__item-meta">{n.type} · {n.id}</div>
          </a>
        ))}
        {!loading && filtered.length === 0 && (
          <div className="sacchi-wiki-panel__empty">Nessuna pagina trovata.</div>
        )}
      </div>
    </div>
  );
}

export default WikiPanel;
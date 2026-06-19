import type { DisplaySource } from '../chat/displays/types';
import type { GraphNode } from './types';

// nodeSources.ts — the PURE projection from a graph node to the Phase-26 Source Explorer
// DisplaySource shape (kept off NodeInspector.tsx so that file only exports a component —
// react-refresh/only-export-components). This is the D-09 cross-link payload: the inspector's
// "open source" action hands `sourcesForNode(node)` to useSourceExplorer().openSources.

/** sourcesForNode projects a graph node into a single DisplaySource row keyed by ref_id. */
export function sourcesForNode(node: GraphNode): readonly DisplaySource[] {
  const rawUrl = node.props?.url;
  const url = typeof rawUrl === 'string' ? rawUrl : undefined;
  const refId = node.ref_id ?? node.id;
  return [
    {
      ref_id: refId,
      index: 1,
      type: 'document',
      title: node.caption ?? refId,
      ...(url !== undefined ? { url } : {}),
      cited: true,
    },
  ];
}

/** isSourceNode is true for a :Document/:Source/:Chunk node — the "open source" deep-link target. */
export function isSourceNode(node: GraphNode): boolean {
  const labels = node.labels ?? [];
  return labels.includes('Document') || labels.includes('Source') || labels.includes('Chunk');
}

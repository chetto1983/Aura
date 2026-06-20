import { getJSON, postJSON } from '../api/json';
import type { GraphIntent, GraphResult, GraphSchema } from './types';

// graphApi.ts is the GRAPH-01 data layer over the plan-02 thin REST adapter
// (GET /api/graph/schema + POST /api/graph/query). It mirrors useConversations.ts'
// getJSON EXACTLY: ALWAYS credentials: 'same-origin' (the SPA is served by the same
// binary that exposes the routes, behind the Phase-24 RequireAuth whole-origin gate),
// Accept: application/json, and a NON-200 — INCLUDING 401 — THROWS `Error("HTTP <n>")`
// rather than returning a discriminated union. Plan-04 consumers drive these through a
// TanStack Query error path so an expired session surfaces a visible auth-error state,
// never a silent blank canvas (requirement B3 / threat T-27-03).
//
// The client authors NO Cypher and builds NO Cypher string — it sends the structured
// GraphIntent ONLY (D-05). There is NO sigma/graphology/@react-sigma import here.

export const GRAPH_SCHEMA_PATH = '/api/graph/schema';
export const GRAPH_QUERY_PATH = '/api/graph/query';

/** GET /api/graph/schema — the live label/rel-type/property-key overview (D-06) that
 * feeds the filters + color legend. Rejects on a non-200 (incl. 401) so the consumer
 * shows an auth/error state, never a silent empty legend (B3 / T-27-03). */
export function fetchGraphSchema(): Promise<GraphSchema> {
  return getJSON<GraphSchema>(GRAPH_SCHEMA_PATH);
}

/** POST /api/graph/query — dispatch a STRUCTURED GraphIntent (never raw Cypher, D-05)
 * and parse the flat {nodes,edges,paths,schema,query} contract. Rejects on a non-200
 * (incl. 401) so the consumer surfaces the auth/error state (B3 / T-27-03). */
export function postGraphQuery(intent: GraphIntent): Promise<GraphResult> {
  return postJSON<GraphResult>(GRAPH_QUERY_PATH, intent);
}

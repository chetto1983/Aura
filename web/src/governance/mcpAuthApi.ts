import { getJSON } from '../api/json';
import { deleteJSON, postJSON } from './governanceApi';

// mcpAuthApi — the cockpit's client for per-identity MCP authorization.
//
// start-then-poll, because the authorization cannot complete inside the request that
// begins it: the human leaves for a consent screen and comes back on a redirect the
// server receives, minutes later and on a different connection. Hermes's dashboard uses
// the same loop (apps/desktop/src/lib/mcp-dashboard-oauth.ts) and the status vocabulary
// below is the server's, which is its.

/** The four states a flow can be in. Anything else is a server we do not understand. */
export type McpFlowStatus = 'starting' | 'authorization_required' | 'approved' | 'error';

export interface McpAuthorizationState {
  /** False for a stdio server, one carrying a static bearer token, or one opted out. */
  readonly supported: boolean;
  readonly authorized: boolean;
  readonly expiresAt?: string;
  /** Why an unsupported server is unsupported, in the operator's terms. */
  readonly reason?: string;
}

export interface McpFlow {
  readonly flowId: string;
  readonly server: string;
  readonly status: McpFlowStatus;
  readonly authorizationUrl?: string;
  readonly redirectUri?: string;
  readonly error?: string;
  readonly expiresAt?: string;
}

function serverPath(name: string): string {
  return `/api/governance/mcp/${encodeURIComponent(name)}/authorization`;
}

export function fetchMcpAuthorization(name: string): Promise<McpAuthorizationState> {
  return getJSON<McpAuthorizationState>(serverPath(name));
}

/**
 * Starts an authorization, or returns the one already in progress. Calling it twice is
 * safe by design — the server replays the pending flow rather than opening a second,
 * which is what keeps a cockpit reload from stranding a consent URL.
 */
export function startMcpAuthorization(name: string): Promise<McpFlow> {
  return postJSON<McpFlow>(serverPath(name));
}

export function pollMcpAuthorizationFlow(flowId: string): Promise<McpFlow> {
  return getJSON<McpFlow>(
    `/api/governance/mcp/authorization/flow/${encodeURIComponent(flowId)}`,
  );
}

export function revokeMcpAuthorization(name: string): Promise<{ readonly removed: boolean }> {
  return deleteJSON<{ readonly removed: boolean }>(serverPath(name));
}

/** How often the cockpit asks whether the human has finished at the provider. */
export const MCP_FLOW_POLL_MS = 1500;

/**
 * How many CONSECUTIVE poll failures to absorb before giving up. A single failed poll is
 * a hiccup — a reloading proxy, a dropped connection — and treating it as a failed
 * authorization would abandon a flow the human is in the middle of completing. Hermes
 * absorbs a small number for the same reason.
 */
export const MCP_FLOW_MAX_POLL_FAILURES = 3;

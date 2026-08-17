import { ToolResultPanel } from '../ToolResultPanel';
import type { DisplayPayload } from './types';
import { TableDisplay } from './TableDisplay';
import { ChartDisplay } from './ChartDisplay';
import { SystemEventCard } from './SystemEventCard';
import { SwarmReportTable } from './SwarmReportTable';
import { LocalArtifactDisplay } from './LocalArtifactDisplay';
import { DocumentDisplay } from './DocumentDisplay';
import { WebResultDisplay } from './WebResultDisplay';
import { CodeDisplay } from './CodeDisplay';
import { McpViewFrame } from '../mcpapps/McpViewFrame';

// DisplayRouter (DISP-02): the single switch(payload.type) entry point, now
// hosted INSIDE the compact ToolActivityCard's expanded body (compact-chat spec
// §3.5) — the collapse boundary moved UP to the tool row; the typed cards
// themselves are unchanged. `system_event`/`local_artifact` bypass the row and
// render inline (the ToolFallback branch in ExternalStoreChat_messages).
//
// SECURITY — D-FALLBACK / HARDEN-08 (T-26-05): the `default:` returns the
// escaped, capped, copyable ToolResultPanel — NEVER null, NEVER a markdown/HTML
// renderer. An unknown or malformed payload.type therefore degrades to escaped
// text, so untrusted output is never lost and never upgraded to a rich render.
// This deliberately OVERRIDES elysia RenderDisplay.tsx's `default: return null`
// (output would be silently dropped).

export interface DisplayRouterProps {
  /** The typed payload the trusted backend normalizer produced. */
  readonly payload: DisplayPayload;
  /** Raw tool props — the D-FALLBACK panel renders the escaped request/result;
   *  toolName/isError stay on the contract for the hosting row's benefit. */
  readonly toolName: string;
  readonly argsText?: string;
  readonly result?: string;
  readonly isError?: boolean;
  /** Citation click-through (26-06): forwarded to the evidence cards' chips so a
   *  click opens the shared read-only Source Explorer for that refId (D-04). */
  readonly onOpenSource?: (refId: string) => void;
}

export function DisplayRouter({ payload, argsText, result, onOpenSource }: DisplayRouterProps) {
  switch (payload.type) {
    // Per-type cases. The "data / status" half (table, chart, system_event,
    // swarm_report, local_artifact) lands in 26-04; the evidence half (web_result,
    // document, code) in 26-05. Each returns its typed display for payload.<slot>.
    case 'table':
      return <TableDisplay payload={payload} />;
    case 'chart':
      return <ChartDisplay payload={payload} />;
    case 'system_event':
      return <SystemEventCard payload={payload} />;
    case 'swarm_report':
      return <SwarmReportTable payload={payload} />;
    case 'local_artifact':
      return <LocalArtifactDisplay payload={payload} />;
    case 'document':
      return <DocumentDisplay payload={payload} {...(onOpenSource ? { onOpenSource } : {})} />;
    case 'web_result':
      return <WebResultDisplay payload={payload} {...(onOpenSource ? { onOpenSource } : {})} />;
    case 'code':
      return <CodeDisplay payload={payload} />;
    // MCP Apps (SEP-1865): a document the mounted server wrote, rendered in a
    // frame on the sandbox origin. A descriptor that did not survive the reducer's
    // narrowing falls through to the escaped panel like any other malformed payload.
    case 'mcp_view':
      return payload.mcp_view ? (
        <McpViewFrame descriptor={payload.mcp_view} />
      ) : (
        <ToolResultPanel argsText={argsText} result={result} />
      );
    default:
      // D-FALLBACK: the escaped structured raw panel, never null (HARDEN-08).
      // ToolActivityCard now HOSTS this router (compact-chat §3.5), so the
      // unknown-type degrade returns the panel directly — no recursion, and the
      // raw output stays escaped, capped, and copyable.
      return <ToolResultPanel argsText={argsText} result={result} />;
  }
}

import { ToolActivityCard } from '../ToolActivityCard';
import type { DisplayPayload } from './types';
import { TableDisplay } from './TableDisplay';
import { ChartDisplay } from './ChartDisplay';
import { SystemEventCard } from './SystemEventCard';
import { SwarmReportTable } from './SwarmReportTable';
import { LocalArtifactDisplay } from './LocalArtifactDisplay';
import { DocumentDisplay } from './DocumentDisplay';
import { WebResultDisplay } from './WebResultDisplay';
import { CodeDisplay } from './CodeDisplay';

// DisplayRouter (DISP-02): the single switch(payload.type) entry point from
// ExternalStoreChat's tools.Fallback. It upgrades a tool turn to a typed display
// when a trusted backend normalizer produced the payload (26-01); the per-type
// case branches land in Wave-2/3 (26-04/26-05).
//
// EXTENSION POINT (for 26-04/26-05): add a `case '<kind>':` that returns the
// per-type display component (e.g. `case 'web_result': return <WebResultDisplay
// payload={payload} />;`). Until then every type falls to the default.
//
// SECURITY — D-FALLBACK / HARDEN-08 (T-26-05): the `default:` returns the
// EXISTING raw, escaped ToolActivityCard — NEVER null, NEVER a markdown/HTML
// renderer. An unknown or malformed payload.type therefore degrades to escaped
// text (the <pre> card), so untrusted output is never lost and never upgraded to
// a rich render. This deliberately OVERRIDES elysia RenderDisplay.tsx's
// `default: return null` (output would be silently dropped).

export interface DisplayRouterProps {
  /** The typed payload the trusted backend normalizer produced. */
  readonly payload: DisplayPayload;
  /** Raw tool props — passed through to the D-FALLBACK card so an unknown type
   *  still renders the full raw tool activity (name + status + escaped result). */
  readonly toolName: string;
  readonly argsText?: string;
  readonly result?: string;
  readonly isError?: boolean;
  /** Citation click-through (26-06): forwarded to the evidence cards' chips so a
   *  click opens the shared read-only Source Explorer for that refId (D-04). */
  readonly onOpenSource?: (refId: string) => void;
}

export function DisplayRouter({
  payload,
  toolName,
  argsText,
  result,
  isError,
  onOpenSource,
}: DisplayRouterProps) {
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
    default:
      // D-FALLBACK: the escaped raw card, never null (HARDEN-08).
      return (
        <ToolActivityCard
          toolName={toolName}
          {...(argsText !== undefined ? { argsText } : {})}
          {...(result !== undefined ? { result } : {})}
          {...(isError !== undefined ? { isError } : {})}
        />
      );
  }
}

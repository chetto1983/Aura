import { ToolResultPanel } from '../ToolResultPanel';
import { McpViewFrame } from '../mcpapps/McpViewFrame';
import type { DisplayPayload } from './types';
import { TableDisplay } from './TableDisplay';
import { ChartDisplay } from './ChartDisplay';
import { SystemEventCard } from './SystemEventCard';
import { SwarmReportTable } from './SwarmReportTable';
import { LocalArtifactDisplay } from './LocalArtifactDisplay';
import { DocumentDisplay } from './DocumentDisplay';
import { WebResultDisplay } from './WebResultDisplay';
import { CodeDisplay } from './CodeDisplay';
import { TodoDisplay } from './TodoDisplay';
import { TerminalDisplay } from './TerminalDisplay';
import { DiffDisplay } from './DiffDisplay';

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
  const raw = () => <ToolResultPanel argsText={argsText} result={result} />;
  switch (payload.type) {
    // Per-type cases. The "data / status" half (table, chart, system_event,
    // swarm_report, local_artifact) lands in 26-04; the evidence half (web_result,
    // document, code) in 26-05. Each returns its typed display for payload.<slot>.
    case 'table':
      return payload.table &&
        Array.isArray(payload.table.columns) &&
        Array.isArray(payload.table.rows) &&
        payload.table.columns.every((cell) => typeof cell === 'string') &&
        payload.table.rows.every(
          (row) => Array.isArray(row) && row.every((cell) => typeof cell === 'string'),
        ) ? (
        <TableDisplay payload={payload} />
      ) : (
        raw()
      );
    case 'chart':
      return payload.chart &&
        Array.isArray(payload.chart.x_labels) &&
        Array.isArray(payload.chart.y_values) ? (
        <ChartDisplay payload={payload} />
      ) : (
        raw()
      );
    case 'system_event':
      return payload.system && typeof payload.system.class === 'string' ? (
        <SystemEventCard payload={payload} />
      ) : (
        raw()
      );
    case 'swarm_report':
      return Array.isArray(payload.swarm) ? <SwarmReportTable payload={payload} /> : raw();
    case 'local_artifact':
      return payload.artifact && typeof payload.artifact.filename === 'string' ? (
        <LocalArtifactDisplay payload={payload} />
      ) : (
        raw()
      );
    case 'document':
      return payload.document && typeof payload.document.content_md === 'string' ? (
        <DocumentDisplay payload={payload} {...(onOpenSource ? { onOpenSource } : {})} />
      ) : (
        raw()
      );
    case 'web_result':
      return Array.isArray(payload.web_results) ? (
        <WebResultDisplay payload={payload} {...(onOpenSource ? { onOpenSource } : {})} />
      ) : (
        raw()
      );
    case 'code':
      return payload.code &&
        typeof payload.code.body === 'string' &&
        (payload.code.filename === undefined || typeof payload.code.filename === 'string') &&
        (payload.code.first_line === undefined ||
          (Number.isInteger(payload.code.first_line) && payload.code.first_line > 0)) &&
        (payload.code.notice === undefined || typeof payload.code.notice === 'string') &&
        (payload.code.extracted === undefined || typeof payload.code.extracted === 'boolean') ? (
        <CodeDisplay payload={payload} {...(result !== undefined ? { rawResult: result } : {})} />
      ) : (
        raw()
      );
    case 'todo':
      return payload.todo &&
        Array.isArray(payload.todo.items) &&
        payload.todo.items.length <= 100 &&
        payload.todo.items.every(
          (item) =>
            item !== null &&
            typeof item === 'object' &&
            typeof item.content === 'string' &&
            item.content.length <= 512 &&
            ['pending', 'in_progress', 'completed'].includes(item.status) &&
            (item.active_form === undefined ||
              (typeof item.active_form === 'string' && item.active_form.length <= 512)),
        ) &&
        payload.todo.items.filter((item) => item.status === 'in_progress').length <= 1 ? (
        <TodoDisplay payload={payload} />
      ) : (
        raw()
      );
    case 'terminal':
      return payload.terminal &&
        typeof payload.terminal.command === 'string' &&
        payload.terminal.command.length > 0 &&
        payload.terminal.command.length <= 4096 &&
        typeof payload.terminal.output === 'string' &&
        payload.terminal.output.length <= 65536 &&
        Number.isInteger(payload.terminal.exit_code) &&
        payload.terminal.exit_code >= 0 &&
        payload.terminal.exit_code <= 255 &&
        (payload.terminal.cwd === undefined || typeof payload.terminal.cwd === 'string') &&
        (payload.terminal.duration_ms === undefined ||
          (Number.isInteger(payload.terminal.duration_ms) && payload.terminal.duration_ms >= 0)) &&
        (payload.terminal.truncated === undefined ||
          typeof payload.terminal.truncated === 'boolean') ? (
        <TerminalDisplay payload={payload} />
      ) : (
        raw()
      );
    case 'diff':
      return payload.diff &&
        typeof payload.diff.filename === 'string' &&
        payload.diff.filename.length > 0 &&
        payload.diff.filename.length <= 1024 &&
        Number.isInteger(payload.diff.additions) &&
        Number.isInteger(payload.diff.deletions) &&
        payload.diff.additions >= 0 &&
        payload.diff.deletions >= 0 &&
        Array.isArray(payload.diff.lines) &&
        payload.diff.lines.length > 0 &&
        payload.diff.lines.length <= 1000 &&
        payload.diff.lines.every(
          (line) =>
            line !== null &&
            typeof line === 'object' &&
            ['context', 'added', 'removed'].includes(line.kind) &&
            typeof line.text === 'string' &&
            line.text.length <= 65536,
        ) &&
        payload.diff.lines.reduce((total, line) => total + line.text.length, 0) <= 65536 &&
        payload.diff.lines.filter((line) => line.kind === 'added').length ===
          payload.diff.additions &&
        payload.diff.lines.filter((line) => line.kind === 'removed').length ===
          payload.diff.deletions ? (
        <DiffDisplay payload={payload} {...(result !== undefined ? { rawResult: result } : {})} />
      ) : (
        raw()
      );
    // MCP Apps (SEP-1865): a document the mounted server wrote, rendered in a
    // frame on the sandbox origin. A descriptor that did not survive the reducer's
    // narrowing falls through to the escaped panel like any other malformed payload.
    case 'mcp_view':
      return payload.mcp_view ? <McpViewFrame descriptor={payload.mcp_view} /> : raw();
    default:
      // D-FALLBACK: the escaped structured raw panel, never null (HARDEN-08).
      // ToolActivityCard now HOSTS this router (compact-chat §3.5), so the
      // unknown-type degrade returns the panel directly — no recursion, and the
      // raw output stays escaped, capped, and copyable.
      return raw();
  }
}

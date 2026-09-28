import { ToolResultPanel } from '../ToolResultPanel';
import { McpViewFrame } from '../mcpapps/McpViewFrame';
import type { DisplayDiff, DisplayPayload, DisplayStats, DisplayTodo } from './types';
import { TableDisplay } from './TableDisplay';
import { MemoryFactsDisplay } from './MemoryFactsDisplay';
import { MemoryStatsDisplay } from './MemoryStatsDisplay';
import { SidecarTableDisplay } from './SidecarTableDisplay';
import { sidecarTableSpec } from './sidecarTableSpec';
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
    case 'table': {
      const table = payload.table;
      if (
        !table ||
        !Array.isArray(table.columns) ||
        !Array.isArray(table.rows) ||
        !table.columns.every((cell) => typeof cell === 'string') ||
        !table.rows.every(
          (row) => Array.isArray(row) && row.every((cell) => typeof cell === 'string'),
        ) ||
        (table.omitted_rows !== undefined &&
          (!Number.isInteger(table.omitted_rows) || table.omitted_rows < 0))
      ) {
        return raw();
      }
      if (payload.title === 'memory_facts' || payload.title === 'memory_entities') {
        return isMemoryTable(payload) ? <MemoryFactsDisplay payload={payload} /> : raw();
      }
      const sidecar = sidecarTableSpec(payload.title);
      if (sidecar) {
        return table.columns.length === sidecar.columns.length &&
          table.rows.every((row) => row.length === sidecar.columns.length) ? (
          <SidecarTableDisplay payload={payload} />
        ) : (
          raw()
        );
      }
      return <TableDisplay payload={payload} />;
    }
    case 'stats':
      return payload.title === 'memory_graph' && isMemoryStats(payload.stats) ? (
        <MemoryStatsDisplay payload={payload} />
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
      return isTodo(payload.todo) ? <TodoDisplay payload={payload} /> : raw();
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
        <TerminalDisplay terminal={payload.terminal} />
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
        hasDiffLines(payload.diff) ? (
        <DiffDisplay diff={payload.diff} {...(result !== undefined ? { rawResult: result } : {})} />
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

function isMemoryTable(payload: DisplayPayload): boolean {
  const table = payload.table;
  if (!table) return false;
  const expectedColumns = payload.title === 'memory_entities' ? 4 : 7;
  return (
    table.columns.length === expectedColumns &&
    table.rows.every((row) => row.length === expectedColumns)
  );
}

function isMemoryStats(value: unknown): value is DisplayStats {
  if (typeof value !== 'object' || value === null || !('items' in value)) return false;
  const items: unknown = value.items;
  if (!Array.isArray(items) || items.length === 0 || items.length > 4) return false;
  const labels = new Set<string>();
  for (const item of items as unknown[]) {
    if (typeof item !== 'object' || item === null || !('label' in item) || !('value' in item)) {
      return false;
    }
    const { label, value: count } = item;
    if (
      typeof label !== 'string' ||
      !['nodes', 'edges', 'isolated_nodes', 'zero_out_degree'].includes(label) ||
      labels.has(label) ||
      typeof count !== 'number' ||
      !Number.isSafeInteger(count) ||
      count < 0
    ) {
      return false;
    }
    labels.add(label);
  }
  return true;
}

/** A todo or diff row as the wire may send it: every field unknown until checked. */
interface WireRow {
  readonly content?: unknown;
  readonly status?: unknown;
  readonly active_form?: unknown;
  readonly kind?: unknown;
  readonly text?: unknown;
}

/** A list slot's rows as the wire sent them: `Array.isArray` narrows a typed list to `any[]`, so
 *  each row is read as unknown, field by field, before a card trusts it. A non-object row is
 *  undefined here. */
function wireRows(list: unknown): readonly (WireRow | undefined)[] | undefined {
  if (!Array.isArray(list)) return undefined;
  return (list as readonly unknown[]).map((row) =>
    typeof row === 'object' && row !== null ? (row as WireRow) : undefined,
  );
}

const TODO_STATUSES: readonly unknown[] = ['pending', 'in_progress', 'completed'];
const TODO_TEXT_MAX = 512;

function isTodoItem(item: WireRow | undefined): item is DisplayTodo['items'][number] {
  return (
    item !== undefined &&
    typeof item.content === 'string' &&
    item.content.length <= TODO_TEXT_MAX &&
    TODO_STATUSES.includes(item.status) &&
    (item.active_form === undefined ||
      (typeof item.active_form === 'string' && item.active_form.length <= TODO_TEXT_MAX))
  );
}

/** todo_write's list: at most 100 well-formed rows, at most one of them in progress. */
function isTodo(todo: DisplayTodo | undefined): boolean {
  const items = wireRows(todo?.items);
  if (items === undefined || items.length > 100 || !items.every(isTodoItem)) return false;
  return items.filter((item) => item.status === 'in_progress').length <= 1;
}

const DIFF_KINDS: readonly unknown[] = ['context', 'added', 'removed'];
const DIFF_TEXT_MAX = 65536;

function isDiffLine(line: WireRow | undefined): line is DisplayDiff['lines'][number] {
  return (
    line !== undefined &&
    DIFF_KINDS.includes(line.kind) &&
    typeof line.text === 'string' &&
    line.text.length <= DIFF_TEXT_MAX
  );
}

/** A patch's lines: 1 to 1000 well-formed rows, 64 KiB of text in all, and exactly as many added
 *  and removed rows as the diff's counts claim. */
function hasDiffLines(diff: DisplayDiff): boolean {
  const lines = wireRows(diff.lines);
  if (lines === undefined || lines.length === 0 || lines.length > 1000) return false;
  if (!lines.every(isDiffLine)) return false;
  return (
    lines.reduce((total, line) => total + line.text.length, 0) <= DIFF_TEXT_MAX &&
    lines.filter((line) => line.kind === 'added').length === diff.additions &&
    lines.filter((line) => line.kind === 'removed').length === diff.deletions
  );
}

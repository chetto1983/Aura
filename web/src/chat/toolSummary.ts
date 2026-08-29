import type { DisplayPayload } from './displays/types';
import { resolveLang } from './displays/shiki';

// toolSummary — pure collapsed-row projections for the compact tool card
// (compact-chat spec §3.2 args summarization + §3.4 result meta). NO React, NO
// i18n side effects: summaries are plain strings the row renders as escaped
// text (HARDEN-08 — never markdown/HTML), metas are key+count descriptors the
// component passes through t(). Structured-JSON parsing only — no NLP, no
// regex over natural language.

/** Hard cap against pathological args; the row also CSS-truncates. */
const SUMMARY_MAX = 120;
const COMMAND_MAX = 64;

function parseArgs(argsText: string): Record<string, unknown> | null {
  const candidates = [argsText, `${argsText}"}`, `${argsText}}`];
  for (const candidate of candidates) {
    try {
      const parsed: unknown = JSON.parse(candidate);
      if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      // partial mid-stream JSON — try the next best-effort closer
    }
  }
  return null;
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined;
}

function firstString(args: Record<string, unknown>): string | undefined {
  for (const value of Object.values(args)) {
    const s = str(value);
    if (s !== undefined) return s;
  }
  return undefined;
}

export function cap(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

function firstLine(value: string): string {
  const nl = value.indexOf('\n');
  return nl >= 0 ? value.slice(0, nl) : value;
}

/**
 * One-line humanized args summary per tool family (§3.2). Returns '' while the
 * streamed JSON is not yet parseable — the summary appears when it is.
 */
export function summarizeArgs(toolName: string, argsText: string): string {
  if (argsText.length === 0) return '';
  const args = parseArgs(argsText);
  if (args === null) return '';

  let out: string | undefined;
  if (toolName.endsWith('_search')) {
    out = str(args.query);
  } else if (toolName.startsWith('fs_')) {
    out = str(args.path);
  } else if (toolName === 'shell_exec' || toolName === 'sandbox_exec') {
    const command = str(args.command);
    out = command === undefined ? undefined : cap(firstLine(command), COMMAND_MAX);
  } else if (toolName.startsWith('memory_')) {
    const verb = toolName.slice('memory_'.length);
    const subject = str(args.query) ?? str(args.key) ?? '';
    out = subject.length > 0 ? `${verb} "${subject}"` : verb;
  } else if (toolName === 'send_file') {
    out = str(args.filename) ?? str(args.path);
  } else if (toolName.includes('.')) {
    // MCP tools (server.tool): the first string-valued property is the subject.
    out = firstString(args);
  } else {
    const first = firstString(args);
    out = first === undefined ? undefined : cap(first, COMMAND_MAX);
  }
  return out === undefined ? '' : cap(out, SUMMARY_MAX);
}

/**
 * The collapsed-row meta descriptor (§3.4): a count-interpolated i18n key, or a
 * literal text (document titles), rendered as `t(key, {count})` / text by the
 * row. `prefix` carries the resolved code lang ("json · 12 lines").
 */
export interface ToolResultMeta {
  readonly key?: string;
  readonly count?: number;
  readonly text?: string;
  readonly prefix?: string;
}

export function resultMeta(
  display: DisplayPayload | undefined,
  result: string | undefined,
): ToolResultMeta | null {
  if (display !== undefined) {
    switch (display.type) {
      case 'web_result':
        return { key: 'chat.tool.meta.results', count: display.web_results?.length ?? 0 };
      case 'table':
        return { key: 'display.table.rowCount', count: display.table?.rows.length ?? 0 };
      case 'code': {
        const body = display.code?.body ?? '';
        const count = body.length === 0 ? 0 : body.split('\n').length;
        const lang = resolveLang(display.code?.lang);
        return { key: 'chat.tool.meta.lines', count, ...(lang !== null ? { prefix: lang } : {}) };
      }
      case 'document': {
        const title = display.document?.title;
        return title !== undefined && title.length > 0
          ? { text: title }
          : { key: 'display.document.untitled' };
      }
      case 'chart':
        return { key: 'chat.tool.meta.points', count: display.chart?.y_values.length ?? 0 };
      case 'swarm_report':
        return { key: 'chat.tool.meta.workers', count: display.swarm?.length ?? 0 };
      default:
        break;
    }
  }
  if (result !== undefined && result.length > 0) {
    return { key: 'chat.tool.meta.chars', count: result.length };
  }
  return null;
}

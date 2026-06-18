// DisplayPayload — the TypeScript wire mirror of the Go `display.Payload` tagged
// union produced by the backend trust boundary (internal/agent/display, Phase
// 26-01). The backend is the ONLY producer of a rich-renderable payload
// (HARDEN-08): a tool result becomes a typed Payload solely when a trusted Go
// normalizer recognized it. The cockpit attaches the payload to a tool part by
// `tool_call_id` (sseAdapter CUSTOM/aura.display frame) and the DisplayRouter
// switches on `type` — defaulting to the escaped raw card when the type is
// unknown, so untrusted output is never upgraded to a rich render.
//
// Field-for-field with 26-01-SUMMARY "DisplayPayload wire shape". Keys are
// snake_case (the Go JSON tags); the frontend reads them verbatim — no camelCase
// remap, so the contract drift surface is exactly one literal pin test.

/** The discriminant — one per Go `display.Kind` constant. */
export type DisplayKind =
  | 'web_result'
  | 'document'
  | 'code'
  | 'local_artifact'
  | 'table'
  | 'chart'
  | 'system_event'
  | 'swarm_report';

/** A single web-search result row (type=web_result). Mirrors display.WebItem. */
export interface WebItem {
  readonly title: string;
  readonly url: string;
  readonly snippet?: string;
  readonly domain?: string;
  readonly score?: number;
  readonly published_at?: string;
  readonly thumbnail?: string;
  readonly ref_id?: string;
}

/** A fetched document (type=document). Mirrors display.Document. */
export interface DisplayDocument {
  readonly title?: string;
  readonly url?: string;
  readonly content_md: string;
}

/** A code/shell body (type=code). Mirrors display.Code. */
export interface DisplayCode {
  readonly body: string;
  readonly lang?: string;
}

/** A produced file (type=local_artifact). Mirrors display.Artifact. */
export interface DisplayArtifact {
  readonly filename: string;
  readonly size_bytes?: number;
  readonly path?: string;
}

/** Structured rows (type=table). Mirrors display.Table. */
export interface DisplayTable {
  readonly columns: readonly string[];
  readonly rows: readonly (readonly string[])[];
}

/** A numeric series (type=chart). Mirrors display.Chart (swap-ready, D-02). */
export interface DisplayChart {
  readonly x_labels: readonly string[];
  readonly y_values: readonly number[];
  readonly x_axis_label?: string;
}

/** A classified safe-reason event (type=system_event). Mirrors display.System. */
export interface DisplaySystem {
  readonly class: string;
  readonly reason?: string;
  readonly message?: string;
  readonly severity: 'error' | 'warning';
}

/** A swarm child report (type=swarm_report). Wire-identical to swarm.ChildReport. */
export interface DisplayChildReport {
  readonly goal_index: number;
  readonly child_id: string;
  readonly status: 'ok' | 'failed' | 'needs_user_input';
  readonly summary?: string;
  readonly error?: string;
  readonly question?: string;
  readonly options?: readonly string[];
  readonly tool_call_id?: string;
}

/** A registry source entry (D-05) — powers citation hovercards + the explorer. */
export interface DisplaySource {
  readonly ref_id: string;
  readonly index: number;
  readonly type?: DisplayKind;
  readonly title?: string;
  readonly url?: string;
  readonly snippet?: string;
  readonly confidence?: number;
  readonly cited: boolean;
}

/**
 * The flat tagged union the backend emits on the `aura.display` CUSTOM event and
 * attaches per tool turn in the MESSAGES_SNAPSHOT replay. `tool_call_id` is the
 * sseAdapter correlation key; `type` is the DisplayRouter discriminant; the
 * per-type slot (web_results / document / …) carries the data for that `type`.
 */
export interface DisplayPayload {
  readonly type: DisplayKind;
  readonly tool_call_id: string;
  readonly title?: string;
  readonly web_results?: readonly WebItem[];
  readonly document?: DisplayDocument;
  readonly code?: DisplayCode;
  readonly artifact?: DisplayArtifact;
  readonly table?: DisplayTable;
  readonly chart?: DisplayChart;
  readonly system?: DisplaySystem;
  readonly swarm?: readonly DisplayChildReport[];
  readonly sources?: readonly DisplaySource[];
}

/** Narrow an unknown value to a DisplayPayload (a string `tool_call_id` + a string `type`). */
export function isDisplayPayload(value: unknown): value is DisplayPayload {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as { type?: unknown; tool_call_id?: unknown };
  return typeof candidate.type === 'string' && typeof candidate.tool_call_id === 'string';
}

import type { AguiFrame } from './sseAdapter_frames';

// sseAdapter_elicitation reads the two CUSTOM frames a mounted MCP server's form travels as
// (internal/agui/run_elicitation.go): aura.elicitation carries the question and
// aura.elicitation_resolved how it closed. Neither carries an answer value. Like aura.steer
// they are a PUMP-level signal, fired from streamSSE and sseResume's pumpBody and never reduced
// into a message part: a form is not part of the assistant's answer.

export type ElicitationKind = 'string' | 'number' | 'integer' | 'boolean' | 'enum';
export type ElicitationFormat = 'email' | 'uri' | 'date' | 'date-time';
export type ElicitationRefusal = 'unrenderable' | 'ambiguous_run';
export type ElicitationAction = 'accept' | 'decline' | 'cancel';

export interface ElicitationField {
  readonly name: string;
  readonly title?: string;
  readonly description?: string;
  readonly kind: ElicitationKind;
  readonly required: boolean;
  readonly default?: unknown;
  readonly enum?: readonly string[];
  readonly enum_titles?: readonly string[];
  readonly multi?: boolean;
  readonly format?: ElicitationFormat;
  readonly min?: number;
  readonly max?: number;
  readonly min_length?: number;
  readonly max_length?: number;
  readonly min_items?: number;
  readonly max_items?: number;
}

export interface ElicitationQuestion {
  readonly run_id: string;
  readonly id: string;
  /** The name Aura mounted the server under, never one the server gave itself. */
  readonly server: string;
  readonly tool?: string;
  readonly message: string;
  readonly fields: readonly ElicitationField[];
  readonly deadline: string;
  readonly refusal?: ElicitationRefusal;
}

export interface ElicitationResolved {
  readonly id: string;
  /** An expiry is a cancel with `expired` set: the server hears cancel either way. */
  readonly action: ElicitationAction;
  readonly expired?: boolean;
}

export type ElicitationSignal =
  | { readonly kind: 'question'; readonly question: ElicitationQuestion }
  | { readonly kind: 'resolved'; readonly resolved: ElicitationResolved };

const KINDS: readonly string[] = ['string', 'number', 'integer', 'boolean', 'enum'];
const FORMATS: readonly string[] = ['email', 'uri', 'date', 'date-time'];
const REFUSALS: readonly string[] = ['unrenderable', 'ambiguous_run'];
const ACTIONS: readonly string[] = ['accept', 'decline', 'cancel'];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function isStringList(value: unknown): value is readonly string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === 'string');
}

function optional(value: unknown, holds: (value: unknown) => boolean): boolean {
  return value === undefined || holds(value);
}

const isString = (value: unknown) => typeof value === 'string';
const isNumber = (value: unknown) => typeof value === 'number';

function isField(value: unknown): value is ElicitationField {
  return (
    isRecord(value) &&
    typeof value.name === 'string' &&
    typeof value.kind === 'string' &&
    KINDS.includes(value.kind) &&
    typeof value.required === 'boolean' &&
    optional(value.title, isString) &&
    optional(value.description, isString) &&
    optional(value.enum, isStringList) &&
    optional(value.enum_titles, isStringList) &&
    optional(value.multi, (multi) => typeof multi === 'boolean') &&
    optional(value.format, (format) => typeof format === 'string' && FORMATS.includes(format)) &&
    [value.min, value.max, value.min_length, value.max_length, value.min_items, value.max_items]
      .map((bound) => optional(bound, isNumber))
      .every(Boolean)
  );
}

/** Narrow one question as the stream and GET /agent/runs/{runID}/elicitations both carry it. */
export function elicitationQuestionOf(value: unknown): ElicitationQuestion | null {
  if (!isRecord(value)) return null;
  const { run_id: runId, id, server, tool, message, deadline, refusal } = value;
  if (typeof runId !== 'string' || typeof id !== 'string' || typeof server !== 'string') {
    return null;
  }
  if (typeof message !== 'string' || typeof deadline !== 'string' || !optional(tool, isString)) {
    return null;
  }
  if (!optional(refusal, (code) => typeof code === 'string' && REFUSALS.includes(code))) {
    return null;
  }
  // A refused form's fields arrive as null: Go encodes the refusal's nil slice that way.
  const fields = refusal !== undefined && value.fields === null ? [] : value.fields;
  if (!Array.isArray(fields) || !fields.every(isField)) return null;
  return {
    run_id: runId,
    id,
    server,
    message,
    deadline,
    fields,
    ...(typeof tool === 'string' ? { tool } : {}),
    ...(typeof refusal === 'string' ? { refusal: refusal as ElicitationRefusal } : {}),
  };
}

function resolvedOf(value: unknown): ElicitationResolved | null {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.action !== 'string') {
    return null;
  }
  if (!ACTIONS.includes(value.action)) return null;
  if (!optional(value.expired, (expired) => typeof expired === 'boolean')) return null;
  return {
    id: value.id,
    action: value.action as ElicitationAction,
    ...(value.expired === true ? { expired: true } : {}),
  };
}

/** Narrow a frame to its elicitation signal, or null: the aura.steer twin (steerNoticeValue). */
export function elicitationSignalValue(frame: AguiFrame): ElicitationSignal | null {
  if (frame.type !== 'CUSTOM') return null;
  if (frame.name === 'aura.elicitation') {
    const question = elicitationQuestionOf(frame.value);
    return question === null ? null : { kind: 'question', question };
  }
  if (frame.name === 'aura.elicitation_resolved') {
    const resolved = resolvedOf(frame.value);
    return resolved === null ? null : { kind: 'resolved', resolved };
  }
  return null;
}

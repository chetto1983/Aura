import { isStringList, type ElicitationField } from '../chat/sseAdapter_elicitation';
import type { ReceiptLine } from './QuestionReceipt';

// elicitationSteps is the MCP form's pure logic: where each step starts, whether it can go on,
// what an accept sends, and what the Review step and the receipt show. Kept out of the .tsx
// files so those export only components.

export type FieldValue = string | number | boolean | readonly string[];
export type FieldValues = Readonly<Record<string, FieldValue | undefined>>;

export interface BooleanLabels {
  readonly yes: string;
  readonly no: string;
}

export interface StepOption {
  readonly id: string;
  readonly label: string;
}

/** A hint's i18n key under `questionCard.choose` and its values. */
export interface ItemsHint {
  readonly key: 'range' | 'atLeast' | 'atMost';
  readonly params: Readonly<Record<string, number>>;
}

const pad = (n: number) => String(n).padStart(2, '0');

/**
 * An RFC 3339 instant as the local YYYY-MM-DDTHH:mm:ss a datetime-local input shows: the input
 * blanks any other shape, so an unconverted default would never be seen.
 */
export function localDateTime(instant: string): string | undefined {
  const at = new Date(instant);
  if (Number.isNaN(at.getTime())) return undefined;
  const date = `${String(at.getFullYear())}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
  return `${date}T${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`;
}

/** A field's starting value: the server's default when it fits the field, else nothing. */
export function initialValue(field: ElicitationField): FieldValue | undefined {
  const { default: fallback } = field;
  switch (field.kind) {
    case 'boolean':
      return typeof fallback === 'boolean' ? fallback : undefined;
    case 'number':
    case 'integer':
      return typeof fallback === 'number' ? fallback : undefined;
    case 'enum':
      if (field.multi === true) return isStringList(fallback) ? fallback : undefined;
      return typeof fallback === 'string' ? fallback : undefined;
    case 'string':
      if (typeof fallback !== 'string') return undefined;
      return field.format === 'date-time' ? localDateTime(fallback) : fallback;
  }
}

export function initialValues(fields: readonly ElicitationField[]): FieldValues {
  return Object.fromEntries(fields.map((field) => [field.name, initialValue(field)]));
}

export function hasValue(value: FieldValue | undefined): boolean {
  if (value === undefined) return false;
  if (typeof value === 'string') return value.trim() !== '';
  if (isStringList(value)) return value.length > 0;
  return true;
}

/** A multi choice inside its item bounds. An optional one left empty is inside: it is skipped. */
export function withinItemBounds(field: ElicitationField, value: FieldValue | undefined): boolean {
  if (field.multi !== true) return true;
  const count = isStringList(value) ? value.length : 0;
  if (count === 0 && !field.required) return true;
  return count >= (field.min_items ?? 0) && count <= (field.max_items ?? Infinity);
}

/** The "Choose 1 to 3." hint a bounded multi choice shows, or null. */
export function itemsHint(field: ElicitationField): ItemsHint | null {
  const { min_items: min, max_items: max } = field;
  if (field.multi !== true) return null;
  if (min !== undefined && max !== undefined) return { key: 'range', params: { min, max } };
  if (min !== undefined) return { key: 'atLeast', params: { min } };
  if (max !== undefined) return { key: 'atMost', params: { max } };
  return null;
}

/**
 * The content an accept sends: every field with a value. A datetime-local value becomes the
 * RFC 3339 instant the server's date-time check reads (internal/elicit/validate.go).
 */
export function contentFrom(
  fields: readonly ElicitationField[],
  values: FieldValues,
): Record<string, unknown> {
  const entries: [string, unknown][] = [];
  for (const field of fields) {
    const value = values[field.name];
    if (value === undefined || !hasValue(value)) continue;
    const instant = field.format === 'date-time' && typeof value === 'string';
    entries.push([field.name, instant ? new Date(value).toISOString() : value]);
  }
  return Object.fromEntries(entries);
}

export function fieldTitle(field: ElicitationField): string {
  return field.title !== undefined && field.title !== '' ? field.title : field.name;
}

/** The rows an enum or a boolean field shows. An option with no title reads as its value. */
export function optionsFor(field: ElicitationField, labels: BooleanLabels): StepOption[] {
  if (field.kind === 'boolean') {
    return [
      { id: 'true', label: labels.yes },
      { id: 'false', label: labels.no },
    ];
  }
  return (field.enum ?? []).map((value, index) => {
    const title = field.enum_titles?.[index];
    return { id: value, label: title !== undefined && title !== '' ? title : value };
  });
}

export function selectedIds(value: FieldValue | undefined): ReadonlySet<string> {
  if (typeof value === 'boolean') return new Set([String(value)]);
  if (typeof value === 'string') return new Set([value]);
  if (isStringList(value)) return new Set(value);
  return new Set();
}

/** A row chosen: a boolean becomes true or false, a single choice its value, and a multi
 *  choice gains or loses it. */
export function toggleValue(
  field: ElicitationField,
  value: FieldValue | undefined,
  id: string,
): FieldValue {
  if (field.kind === 'boolean') return id === 'true';
  if (field.multi !== true) return id;
  const chosen = isStringList(value) ? value : [];
  return chosen.includes(id) ? chosen.filter((entry) => entry !== id) : [...chosen, id];
}

/** The step a 422 sends the card back to: the first refused field, else the first step. */
export function firstFailingStep(
  fields: readonly ElicitationField[],
  errors: Readonly<Record<string, string>>,
): number {
  return Math.max(
    fields.findIndex((field) => Object.hasOwn(errors, field.name)),
    0,
  );
}

/**
 * What the server receives for a field, as the operator reads it, or undefined for nothing.
 * An optional field left empty still arrives with its default: go-sdk puts the default back
 * after the handler (mcp/client.go:901), and a default that does not fit the field is sent as
 * written.
 */
export function receivedText(
  field: ElicitationField,
  value: FieldValue | undefined,
  labels: BooleanLabels,
): string | undefined {
  if (value !== undefined && hasValue(value)) return display(field, value, labels);
  if (field.required || field.default === undefined) return undefined;
  const fallback = initialValue(field);
  return fallback === undefined ? JSON.stringify(field.default) : display(field, fallback, labels);
}

/** The receipt's lines: each field the server receives, as the operator saw it. */
export function summaryOf(
  fields: readonly ElicitationField[],
  values: FieldValues,
  labels: BooleanLabels,
): ReceiptLine[] {
  return fields.flatMap((field) => {
    const value = receivedText(field, values[field.name], labels);
    return value === undefined ? [] : [{ label: fieldTitle(field), value }];
  });
}

function display(field: ElicitationField, value: FieldValue, labels: BooleanLabels): string {
  if (typeof value === 'boolean') return value ? labels.yes : labels.no;
  if (typeof value === 'number') return String(value);
  const ids = typeof value === 'string' ? [value] : value;
  if (field.kind !== 'enum') return ids.join(', ');
  const options = optionsFor(field, labels);
  return ids.map((id) => options.find((option) => option.id === id)?.label ?? id).join(', ');
}

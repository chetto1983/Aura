import type { DiffValue, DocumentRow, FieldValue, FieldsDiff } from './documentsApi';

/** The fields holding one value each, in the order a diff lists them; custom fields follow. */
export const SINGLE_FIELDS = ['type', 'counterparty', 'document_date'] as const;

export const TAG_FIELD = 'tag';

/** The detail's form: a key per single-valued field, '' holding nothing ("custom:<id>" names a
 * custom field), and the tags. It holds only what the operator set or accepted; a suggestion
 * enters it with a click. */
export interface DetailForm {
  readonly single: Readonly<Record<string, string>>;
  readonly tags: readonly string[];
}

export function isReviewed(value: FieldValue): boolean {
  return value.set_by === 'operator' || value.accepted;
}

export function formOf(doc: DocumentRow): DetailForm {
  const single: Record<string, string> = {};
  const tags: string[] = [];
  for (const value of doc.values) {
    if (!isReviewed(value)) continue;
    if (value.field === TAG_FIELD) tags.push(value.value_key);
    else single[value.field] = value.value_key;
  }
  return { single, tags };
}

/** suggestionsOf is what a rule or the extractor proposes on one field, waiting for the
 * operator. */
export function suggestionsOf(doc: DocumentRow, field: string): readonly FieldValue[] {
  return doc.values.filter((value) => value.field === field && !isReviewed(value));
}

function fieldRank(field: string): number {
  const rank = (SINGLE_FIELDS as readonly string[]).indexOf(field);
  return rank === -1 ? SINGLE_FIELDS.length : rank;
}

/** formDiff is what Save sends: a set for every field the operator changed, a remove for every
 * field they cleared, and the tags they added or took off. */
export function formDiff(doc: DocumentRow, form: DetailForm): FieldsDiff {
  const held = formOf(doc);
  const set: DiffValue[] = [];
  const remove: DiffValue[] = [];
  const fields = [...new Set([...Object.keys(held.single), ...Object.keys(form.single)])].sort(
    (a, b) => fieldRank(a) - fieldRank(b) || a.localeCompare(b),
  );
  for (const field of fields) {
    const before = held.single[field] ?? '';
    const after = (form.single[field] ?? '').trim();
    if (after === before) continue;
    if (after === '') remove.push({ field, value_key: before });
    else set.push({ field, value_key: after });
  }
  for (const tag of form.tags) {
    if (!held.tags.includes(tag)) set.push({ field: TAG_FIELD, value_key: tag });
  }
  for (const tag of held.tags) {
    if (!form.tags.includes(tag)) remove.push({ field: TAG_FIELD, value_key: tag });
  }
  return { set, remove };
}

/** Apply only the user's changes over refreshed values; untouched fields stay current. */
export function formWithDiff(doc: DocumentRow, diff: FieldsDiff): DetailForm {
  const held = formOf(doc);
  const single = { ...held.single };
  const tags = new Set(held.tags);
  for (const value of diff.remove) {
    if (value.field === TAG_FIELD) tags.delete(value.value_key);
    else single[value.field] = '';
  }
  for (const value of diff.set) {
    if (value.field === TAG_FIELD) tags.add(value.value_key);
    else single[value.field] = value.value_key;
  }
  return { single, tags: [...tags] };
}

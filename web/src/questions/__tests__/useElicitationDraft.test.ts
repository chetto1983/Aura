import { describe, expect, it } from 'vitest';
import type { ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import { readDraft } from '../useElicitationDraft';

const QUESTION: ElicitationQuestion = {
  run_id: 'run-1',
  id: 'q-1',
  server: 'forms',
  message: 'm',
  fields: [
    { name: 'name', kind: 'string', required: true },
    { name: 'pets', kind: 'enum', required: false, multi: true, enum: ['cat', 'dog'] },
    { name: 'age', kind: 'integer', required: false },
    { name: 'agree', kind: 'boolean', required: false },
    { name: 'hero', kind: 'enum', required: false, enum: ['a', 'b'] },
    { name: 'ratio', kind: 'number', required: false },
  ],
  deadline: '2026-11-27T10:05:00Z',
};

const store = (draft: unknown) => {
  sessionStorage.setItem('aura.elicitation.draft.q-1', JSON.stringify(draft));
};

describe('readDraft', () => {
  it('finds nothing without a draft, or with one that does not parse', () => {
    expect(readDraft(QUESTION)).toBeUndefined();
    sessionStorage.setItem('aura.elicitation.draft.q-1', '{not json');
    expect(readDraft(QUESTION)).toBeUndefined();
    store(null);
    expect(readDraft(QUESTION)).toBeUndefined();
    store(5);
    expect(readDraft(QUESTION)).toBeUndefined();
    store({ step: 1 });
    expect(readDraft(QUESTION)).toBeUndefined();
    store({ step: 1, values: null });
    expect(readDraft(QUESTION)).toBeUndefined();
  });

  it('gives back the values of the form, and drops what does not belong to it', () => {
    const kept = { name: 'Ada', pets: ['cat'], age: 36, agree: false, hero: 'b', ratio: 0.5 };
    store({ step: 2, values: { ...kept, extra: 'x', constructor: 'y' } });
    expect(readDraft(QUESTION)).toEqual({ step: 2, values: kept });
  });

  it.each([
    { name: { nested: true }, pets: ['cat', 3], age: '36', agree: 'no', hero: ['a'], ratio: '1' },
    { name: 7, pets: 'cat', age: true, agree: 0, hero: 1, ratio: null },
  ])('drops a value whose type is not its field kind: %o', (values) => {
    store({ step: 0, values });
    expect(Object.values(readDraft(QUESTION)?.values ?? { missing: true })).toEqual(
      Array.from({ length: QUESTION.fields.length }, () => undefined),
    );
  });

  it('holds the step within the form, Review included', () => {
    store({ step: 9, values: {} });
    expect(readDraft(QUESTION)?.step).toBe(QUESTION.fields.length);
    store({ step: -2, values: {} });
    expect(readDraft(QUESTION)?.step).toBe(0);
    store({ step: 1.5, values: {} });
    expect(readDraft(QUESTION)?.step).toBe(0);
    store({ step: 1, values: {} });
    expect(
      readDraft({ ...QUESTION, fields: [{ name: 'name', kind: 'string', required: true }] })?.step,
    ).toBe(0);
  });
});

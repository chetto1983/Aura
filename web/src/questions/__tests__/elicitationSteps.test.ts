import { describe, expect, it } from 'vitest';
import type { ElicitationField } from '../../chat/sseAdapter_elicitation';
import {
  contentFrom,
  fieldTitle,
  firstFailingStep,
  hasValue,
  initialValue,
  itemsHint,
  localDateTime,
  optionsFor,
  receivedText,
  selectedIds,
  summaryOf,
  toggleValue,
  withinItemBounds,
} from '../elicitationSteps';

const LABELS = { yes: 'Yes', no: 'No' };
type Needed = Pick<ElicitationField, 'name' | 'kind'>;
const field = (over: Partial<ElicitationField> & Needed): ElicitationField => ({
  required: false,
  ...over,
});

describe('elicitationSteps', () => {
  it('preserves schema field names that also exist on the Object prototype', () => {
    const fields = [
      field({ name: '__proto__', kind: 'string' }),
      field({ name: 'constructor', kind: 'string' }),
    ];
    const values = Object.fromEntries([
      ['__proto__', 'Ada'],
      ['constructor', 'Bob'],
    ]);
    expect(JSON.stringify(contentFrom(fields, values))).toBe(
      '{"__proto__":"Ada","constructor":"Bob"}',
    );
    expect(firstFailingStep(fields, { constructor: 'required' })).toBe(1);
  });

  it("starts each field at the server's default, when the default fits", () => {
    const multi = { name: 'a', kind: 'enum', multi: true, enum: ['x'] } as const;
    expect(initialValue(field({ name: 'a', kind: 'boolean', default: true }))).toBe(true);
    expect(initialValue(field({ name: 'a', kind: 'boolean', default: 'yes' }))).toBeUndefined();
    expect(initialValue(field({ name: 'a', kind: 'integer', default: 3 }))).toBe(3);
    expect(initialValue(field({ name: 'a', kind: 'string', default: 'blue' }))).toBe('blue');
    expect(initialValue(field({ name: 'a', kind: 'enum', enum: ['x'], default: 'x' }))).toBe('x');
    expect(initialValue(field({ ...multi, default: ['x'] }))).toEqual(['x']);
    expect(initialValue(field({ ...multi, default: 'x' }))).toBeUndefined();
  });

  it('shows a date-time default in the local shape its input accepts, at the same instant', () => {
    const when = field({ name: 'w', kind: 'string', format: 'date-time' });
    const shown = initialValue({ ...when, default: '2026-09-25T10:30:15Z' });
    expect(shown).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/);
    expect(new Date(shown as string).toISOString()).toBe('2026-09-25T10:30:15.000Z');
    expect(localDateTime('not a date')).toBeUndefined();
  });

  it('knows when a step has a value', () => {
    const values = [undefined, '  ', [], 'a', 0, false, ['a']] as const;
    expect(values.map((value) => hasValue(value))).toEqual([
      false,
      false,
      false,
      true,
      true,
      true,
      true,
    ]);
  });

  it('holds a multi choice to its item bounds, and lets an optional one stay empty', () => {
    const bounded = field({ name: 'm', kind: 'enum', multi: true, min_items: 1, max_items: 2 });
    expect(withinItemBounds(bounded, undefined)).toBe(true);
    expect(withinItemBounds({ ...bounded, required: true }, [])).toBe(false);
    expect(withinItemBounds(bounded, ['a'])).toBe(true);
    expect(withinItemBounds(bounded, ['a', 'b', 'c'])).toBe(false);
    expect(withinItemBounds(field({ name: 's', kind: 'string' }), 'x')).toBe(true);
    expect(itemsHint(bounded)).toEqual({ key: 'range', params: { min: 1, max: 2 } });
    const multi = { name: 'm', kind: 'enum', multi: true } as const;
    expect(itemsHint(field({ ...multi, min_items: 1 }))).toEqual({
      key: 'atLeast',
      params: { min: 1 },
    });
    expect(itemsHint(field({ ...multi, max_items: 2 }))).toEqual({
      key: 'atMost',
      params: { max: 2 },
    });
    expect(itemsHint(field(multi))).toBeNull();
    expect(itemsHint(field({ name: 's', kind: 'string', min_items: 1 }))).toBeNull();
  });

  it('sends only what has a value, and a local date-time as an RFC 3339 instant', () => {
    const fields = [
      field({ name: 'when', kind: 'string', format: 'date-time' }),
      field({ name: 'n', kind: 'number' }),
      field({ name: 'empty', kind: 'string' }),
    ];
    const content = contentFrom(fields, { when: '2026-09-25T10:30', n: 2.5, empty: '' });
    expect(content.n).toBe(2.5);
    expect('empty' in content).toBe(false);
    expect(content.when).toBe(new Date('2026-09-25T10:30').toISOString());
  });

  it('draws enum rows with their titles, and a boolean as Yes and No', () => {
    const pet = field({ name: 'p', kind: 'enum', enum: ['cat', 'dog'], enum_titles: ['Cat', ''] });
    expect(optionsFor(pet, LABELS)).toEqual([
      { id: 'cat', label: 'Cat' },
      { id: 'dog', label: 'dog' },
    ]);
    const yesNo = optionsFor(field({ name: 'b', kind: 'boolean' }), LABELS);
    expect(yesNo.map((o) => o.label)).toEqual(['Yes', 'No']);
  });

  it('toggles a row into the value its field keeps', () => {
    const multi = field({ name: 'm', kind: 'enum', multi: true, enum: ['a', 'b'] });
    expect(toggleValue(field({ name: 'b', kind: 'boolean' }), undefined, 'false')).toBe(false);
    expect(toggleValue(field({ name: 's', kind: 'enum', enum: ['a'] }), 'b', 'a')).toBe('a');
    expect(toggleValue(multi, ['a'], 'b')).toEqual(['a', 'b']);
    expect(toggleValue(multi, ['a', 'b'], 'a')).toEqual(['b']);
    expect([...selectedIds(true)]).toEqual(['true']);
    expect([...selectedIds(['a', 'b'])]).toEqual(['a', 'b']);
    expect([...selectedIds(undefined)]).toEqual([]);
  });

  it('goes back to the first refused field, or the first step for a whole-answer error', () => {
    const fields = [field({ name: 'a', kind: 'string' }), field({ name: 'b', kind: 'string' })];
    expect(firstFailingStep(fields, { b: 'invalid' })).toBe(1);
    expect(firstFailingStep(fields, { '': 'not_asked' })).toBe(0);
  });

  it('summarises what the server receives, defaults included', () => {
    const fields = [
      field({ name: 'pet', kind: 'enum', enum: ['cat'], enum_titles: ['Cat'], title: 'Pet' }),
      field({ name: 'agree', kind: 'boolean' }),
      field({ name: 'tags', kind: 'enum', multi: true, enum: ['a', 'b'] }),
      field({ name: 'age', kind: 'integer' }),
      field({ name: 'note', kind: 'string' }),
      field({ name: 'color', kind: 'string', default: 'blue' }),
      field({ name: 'size', kind: 'enum', enum: ['s', 'm'], default: 3 }),
    ];
    const values = { pet: 'cat', agree: false, tags: ['a', 'b'], age: 36 };
    expect(summaryOf(fields, values, LABELS)).toEqual([
      { label: 'Pet', value: 'Cat' },
      { label: 'agree', value: 'No' },
      { label: 'tags', value: 'a, b' },
      { label: 'age', value: '36' },
      { label: 'color', value: 'blue' },
      { label: 'size', value: '3' },
    ]);
    const required = field({ name: 'r', kind: 'string', required: true, default: 'x' });
    expect(receivedText(required, undefined, LABELS)).toBeUndefined();
    expect(fieldTitle(field({ name: 'n', kind: 'string', title: '' }))).toBe('n');
  });
});

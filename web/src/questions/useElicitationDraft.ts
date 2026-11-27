import { useEffect, useState } from 'react';
import {
  isStringList,
  type ElicitationField,
  type ElicitationQuestion,
} from '../chat/sseAdapter_elicitation';
import { readSessionValue, writeSessionValue } from '../lib/sessionValue';
import { initialValues, type FieldValue, type FieldValues } from './elicitationSteps';

// useElicitationDraft keeps a form's step and values across a reload of the tab. A reload brings
// the question back (the replay, or the run's list) but not what the operator had typed: the card
// started again at step 1, empty (measured 2026-11-27). The draft lives in the tab's session
// storage under the question's id, and goes once the form is settled, or with the tab.

const PREFIX = 'aura.elicitation.draft.';

interface Draft {
  readonly step: number;
  readonly values: FieldValues;
}

/** Whether value is what the card itself holds for field (initialValue, FieldInput, toggleValue). */
function fits(field: ElicitationField, value: unknown): value is FieldValue {
  switch (field.kind) {
    case 'boolean':
      return typeof value === 'boolean';
    case 'number':
    case 'integer':
      return typeof value === 'number';
    case 'enum':
      return field.multi === true ? isStringList(value) : typeof value === 'string';
    case 'string':
      return typeof value === 'string';
  }
}

/** The stored draft, cut to the form it belongs to: unknown fields and values are dropped and
 *  the step is held within the form. Undefined when there is none, or it does not parse. */
export function readDraft(question: ElicitationQuestion): Draft | undefined {
  const raw = readSessionValue(PREFIX + question.id);
  if (raw === undefined) return undefined;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return undefined;
  }
  if (typeof parsed !== 'object' || parsed === null) return undefined;
  const { step, values } = parsed as { readonly step?: unknown; readonly values?: unknown };
  if (typeof values !== 'object' || values === null) return undefined;
  const stored = values as Readonly<Record<string, unknown>>;
  const { fields } = question;
  const lastStep = fields.length > 1 ? fields.length : 0;
  return {
    step: Number.isInteger(step) ? Math.min(Math.max(step as number, 0), lastStep) : 0,
    values: Object.fromEntries(
      fields.map((field) => {
        const value = Object.hasOwn(stored, field.name) ? stored[field.name] : undefined;
        return [field.name, fits(field, value) ? value : undefined];
      }),
    ),
  };
}

export function useElicitationDraft(question: ElicitationQuestion, settled: boolean) {
  const [initial] = useState<Draft>(
    () => readDraft(question) ?? { step: 0, values: initialValues(question.fields) },
  );
  const [step, setStep] = useState(initial.step);
  const [values, setValues] = useState<FieldValues>(initial.values);
  const key = PREFIX + question.id;
  useEffect(() => {
    writeSessionValue(key, settled ? undefined : JSON.stringify({ step, values }));
  }, [key, settled, step, values]);
  return { step, setStep, values, setValues };
}

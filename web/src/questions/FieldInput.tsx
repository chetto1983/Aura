import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import type { ElicitationField, ElicitationFormat } from '../chat/sseAdapter_elicitation';
import type { FieldValue } from './elicitationSteps';
import { Input } from '@/components/ui/input';

// FieldInput is a string or number step of an MCP form: an input typed for the field's format,
// or a numeric one with the field's bounds. Enter goes on to the next step.

const INPUT_TYPE: Record<ElicitationFormat, string> = {
  email: 'email',
  uri: 'url',
  date: 'date',
  'date-time': 'datetime-local',
};

export interface FieldInputProps {
  readonly field: ElicitationField;
  readonly labelledBy: string;
  readonly describedBy?: string;
  readonly value: FieldValue | undefined;
  readonly invalid: boolean;
  readonly disabled: boolean;
  readonly onChange: (value: FieldValue | undefined) => void;
  readonly onEnter: () => void;
}

export function FieldInput({
  field,
  labelledBy,
  describedBy,
  value,
  invalid,
  disabled,
  onChange,
  onEnter,
}: FieldInputProps) {
  const { t } = useTranslation();
  const numeric = field.kind === 'number' || field.kind === 'integer';
  const type = numeric ? 'number' : field.format === undefined ? 'text' : INPUT_TYPE[field.format];
  return (
    <Input
      type={type}
      aria-labelledby={labelledBy}
      {...(describedBy !== undefined ? { 'aria-describedby': describedBy } : {})}
      aria-invalid={ariaInvalid(invalid)}
      value={typeof value === 'string' || typeof value === 'number' ? value : ''}
      disabled={disabled}
      placeholder={t('questionCard.placeholder')}
      {...(numeric ? { step: field.kind === 'integer' ? 1 : 'any' } : {})}
      {...(field.format === 'date-time' ? { step: 1 } : {})}
      {...(field.min !== undefined ? { min: field.min } : {})}
      {...(field.max !== undefined ? { max: field.max } : {})}
      {...(field.min_length !== undefined ? { minLength: field.min_length } : {})}
      {...(field.max_length !== undefined ? { maxLength: field.max_length } : {})}
      onChange={(event) => {
        const raw = event.target.value;
        if (raw === '') onChange(undefined);
        else onChange(numeric ? Number(raw) : raw);
      }}
      onKeyDown={(event) => {
        if (event.key !== 'Enter') return;
        event.preventDefault();
        onEnter();
      }}
      className="bg-surface text-sm"
    />
  );
}

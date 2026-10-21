import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { CustomField, DocumentsFilter } from './documentsApi';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

export function CustomValueInput({
  field,
  value,
  onChange,
}: {
  readonly field: CustomField;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  return field.data_type === 'select' ? (
    <Select
      aria-label={field.name}
      value={value}
      options={(field.options ?? []).map((option) => ({ value: option, label: option }))}
      onValueChange={onChange}
    />
  ) : (
    <Input
      aria-label={field.name}
      type={field.data_type === 'date' ? 'date' : field.data_type === 'integer' ? 'number' : 'text'}
      step={field.data_type === 'integer' ? 1 : undefined}
      value={value}
      onChange={(event) => {
        onChange(event.target.value);
      }}
    />
  );
}

export function CustomFilters({
  fields,
  filter,
  onChange,
  showControl = true,
  showChips = true,
}: {
  readonly fields: readonly CustomField[];
  readonly filter: DocumentsFilter;
  readonly onChange: (filter: DocumentsFilter) => void;
  readonly showControl?: boolean;
  readonly showChips?: boolean;
}) {
  const { t } = useTranslation();
  const [fieldId, setFieldId] = useState('');
  const [operator, setOperator] = useState<'eq' | 'unassigned'>('eq');
  const [value, setValue] = useState('');
  const field = fields.find((item) => item.id === fieldId) ?? fields[0];
  return (
    <>
      {showChips &&
        filter.custom.map((item, index) => (
          <Button
            key={`${item.field}:${String(index)}`}
            variant="outline"
            size="sm"
            onClick={() => {
              onChange({ ...filter, custom: filter.custom.filter((_, i) => i !== index) });
            }}
          >
            {fields.find((cf) => `custom:${cf.id}` === item.field)?.name ?? item.field}:{' '}
            {item.operator === 'unassigned' ? t('documents.filters.unassigned') : item.value} ×
          </Button>
        ))}
      {field && showControl && (
        <Popover>
          <PopoverTrigger asChild>
            <Button size="sm" variant="outline">
              {t('documents.workspace.custom')}
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-72 space-y-3">
            <Select
              aria-label={t('documents.columns.fields')}
              value={field.id}
              options={fields.map((cf) => ({ value: cf.id, label: cf.name }))}
              onValueChange={(id) => {
                setFieldId(id);
                setValue('');
              }}
            />
            <Select
              aria-label={t('documents.workspace.custom')}
              value={operator}
              options={[
                { value: 'eq', label: t('documents.workspace.equals') },
                { value: 'unassigned', label: t('documents.filters.unassigned') },
              ]}
              onValueChange={(op) => {
                setOperator(op === 'eq' ? 'eq' : 'unassigned');
              }}
            />
            {operator === 'eq' && (
              <CustomValueInput field={field} value={value} onChange={setValue} />
            )}
            <Button
              disabled={operator === 'eq' && !value.trim()}
              onClick={() => {
                onChange({
                  ...filter,
                  custom: [
                    ...filter.custom.filter((item) => item.field !== `custom:${field.id}`),
                    {
                      field: `custom:${field.id}`,
                      operator,
                      ...(operator === 'eq' ? { value: value.trim() } : {}),
                    },
                  ],
                });
              }}
            >
              {t('documents.bulk.apply')}
            </Button>
          </PopoverContent>
        </Popover>
      )}
    </>
  );
}

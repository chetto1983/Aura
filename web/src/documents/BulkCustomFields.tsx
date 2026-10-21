import { useTranslation } from 'react-i18next';
import { CustomValueInput } from './CustomFilters';
import type { CustomField } from './documentsApi';
import type { BulkPending } from './bulkModel';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

export function BulkCustomFields({
  fields,
  pending,
  onChange,
  canClear,
}: {
  readonly fields: readonly CustomField[];
  readonly pending: BulkPending;
  readonly onChange: (pending: BulkPending) => void;
  readonly canClear: boolean;
}) {
  const { t } = useTranslation();
  const set = (field: string, value: string | null | undefined) => {
    onChange({ ...pending, custom: { ...pending.custom, [field]: value } });
  };
  if (!fields.length) return null;
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="sm" variant="secondary">
          {t('documents.workspace.customEdit')}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="max-h-96 w-80 space-y-4 overflow-y-auto">
        {!canClear && (
          <p className="text-xs text-text-muted">{t('documents.workspace.clearSelectedOnly')}</p>
        )}
        {fields.map((field) => {
          const key = `custom:${field.id}`,
            value = pending.custom?.[key];
          return (
            <div key={field.id} className="space-y-2">
              <p className="text-sm font-semibold">{field.name}</p>
              <Select
                aria-label={`${field.name}: ${t('documents.workspace.customEdit')}`}
                value={value === undefined ? 'unchanged' : value === null ? 'clear' : 'set'}
                options={[
                  { value: 'unchanged', label: t('documents.workspace.unchanged') },
                  { value: 'set', label: t('documents.workspace.setValue') },
                  {
                    value: 'clear',
                    label: t('documents.workspace.clearValue'),
                    disabled: !canClear,
                  },
                ]}
                onValueChange={(choice) => {
                  set(key, choice === 'unchanged' ? undefined : choice === 'clear' ? null : '');
                }}
              />
              {typeof value === 'string' && (
                <CustomValueInput
                  field={field}
                  value={value}
                  onChange={(next) => {
                    set(key, next);
                  }}
                />
              )}
            </div>
          );
        })}
      </PopoverContent>
    </Popover>
  );
}

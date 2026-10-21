import { type ReactNode } from 'react';
import { X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { CustomField, DocumentRow, FieldValue, FilterField } from './documentsApi';
import { suggestionsOf, TAG_FIELD, type DetailForm } from './detailModel';
import type { DetailNames } from './detailNames';
import { SuggestionChip } from './SuggestionChip';
import type { DocumentLists } from './useDocumentLists';
import { DocumentLabelPicker } from './DocumentLabelPicker';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';

interface DetailFieldsProps {
  readonly doc: DocumentRow;
  readonly form: DetailForm;
  readonly onChange: (form: DetailForm) => void;
  readonly onReject: (value: FieldValue) => void;
  readonly lists: DocumentLists['lists'];
  readonly customFields: readonly CustomField[];
  readonly names: DetailNames;
  readonly busy: boolean;
}

const INPUT_TYPE: Readonly<Record<CustomField['data_type'], string>> = {
  date: 'date',
  integer: 'number',
  monetary: 'text',
  text: 'text',
  select: 'text',
};

/**
 * The detail's form (mockup 3): type, counterparty, date, the custom fields and the tags, each
 * with the suggestions waiting under it. A suggestion's click and every edit change the form
 * only; × on a suggestion rejects it at once, which the parent writes.
 */
export function DetailFields(props: DetailFieldsProps) {
  const { doc, form, onChange, onReject, lists, customFields, names, busy } = props;
  const { t } = useTranslation();
  const setSingle = (field: string, key: string) => {
    onChange({ ...form, single: { ...form.single, [field]: key } });
  };
  // A suggestion the operator already took into the form leaves its chips.
  const inForm = (value: FieldValue) =>
    value.field === TAG_FIELD
      ? form.tags.includes(value.value_key)
      : form.single[value.field] === value.value_key;
  const chips = (field: string, take: (value: FieldValue) => void) =>
    suggestionsOf(doc, field)
      .filter((value) => !inForm(value))
      .map((value) => (
        <SuggestionChip
          key={value.value_key}
          value={value}
          name={names.value(field, value.value_key)}
          disabled={busy}
          onUse={() => {
            take(value);
          }}
          onReject={() => {
            onReject(value);
          }}
        />
      ));
  const takeSingle = (value: FieldValue) => {
    setSingle(value.field, value.value_key);
  };

  const labelSelect = (field: 'type' | 'counterparty', list: FilterField) => {
    const current = form.single[field] ?? '';
    return (
      <Field legend={names.field(field)} suggestions={chips(field, takeSingle)}>
        <DocumentLabelPicker
          name={names.field(field)}
          kind={field}
          labels={lists[list]}
          currentName={names.value(field, current)}
          value={current}
          disabled={busy}
          onValueChange={(key) => {
            setSingle(field, key);
          }}
        />
      </Field>
    );
  };

  const textInput = (field: string, type: string) => (
    <Input
      aria-label={names.field(field)}
      type={type}
      value={form.single[field] ?? ''}
      disabled={busy}
      onChange={(event) => {
        setSingle(field, event.target.value);
      }}
    />
  );

  const tagList = lists.tags.filter((label) => !form.tags.includes(label.id));
  return (
    <div className="flex flex-col gap-5">
      {labelSelect('type', 'type')}
      {labelSelect('counterparty', 'counterparty')}
      <Field legend={names.field('document_date')} suggestions={chips('document_date', takeSingle)}>
        {textInput('document_date', 'date')}
      </Field>
      {customFields.map((cf) => {
        const field = `custom:${cf.id}`;
        return (
          <Field key={cf.id} legend={cf.name} suggestions={chips(field, takeSingle)}>
            {cf.data_type === 'select' ? (
              <Select
                aria-label={cf.name}
                value={form.single[field] ?? ''}
                disabled={busy}
                options={[
                  { value: '', label: t('documents.detail.none') },
                  ...(cf.options ?? []).map((option) => ({ value: option, label: option })),
                ]}
                onValueChange={(key) => {
                  setSingle(field, key);
                }}
              />
            ) : (
              textInput(field, INPUT_TYPE[cf.data_type])
            )}
          </Field>
        );
      })}
      <Field
        legend={names.field(TAG_FIELD)}
        suggestions={chips(TAG_FIELD, (value) => {
          onChange({ ...form, tags: [...form.tags, value.value_key] });
        })}
      >
        <div className="flex flex-wrap items-center gap-2">
          {form.tags.map((id) => {
            const name = names.value(TAG_FIELD, id);
            return (
              <span
                key={id}
                className="inline-flex items-center gap-1 rounded-full bg-surface-3 py-0.5 pr-1 pl-3 text-[13px] text-text"
              >
                {name}
                <button
                  type="button"
                  aria-label={t('documents.detail.removeTag', { name })}
                  disabled={busy}
                  onClick={() => {
                    onChange({ ...form, tags: form.tags.filter((tag) => tag !== id) });
                  }}
                  className="grid size-6 place-items-center rounded-full text-text-muted hover:bg-danger/15 hover:text-danger focus-visible:outline-2 focus-visible:outline-ring"
                >
                  <X aria-hidden="true" className="size-3" />
                </button>
              </span>
            );
          })}
          <DocumentLabelPicker
            name={t('documents.detail.addTagLabel')}
            kind="tag"
            labels={tagList}
            value=""
            disabled={busy}
            onValueChange={(id) => {
              onChange({ ...form, tags: [...form.tags, id] });
            }}
          />
        </div>
      </Field>
    </div>
  );
}

/** One field of the form: its name, its control and the suggestions under it. */
function Field({
  legend,
  suggestions,
  children,
}: {
  readonly legend: string;
  readonly suggestions: readonly ReactNode[];
  readonly children: ReactNode;
}) {
  return (
    <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
      <legend className="mb-1.5 p-0 text-xs font-semibold tracking-[0.08em] text-text-muted uppercase">
        {legend}
      </legend>
      {children}
      {suggestions}
    </fieldset>
  );
}

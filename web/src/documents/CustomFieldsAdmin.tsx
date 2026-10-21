import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { AdminEntry, DeleteConfirm, EditorField, EditorFrame, KindChoice } from './adminParts';
import { deleteCustomField, saveCustomField, type CustomField } from './documentsApi';
import { useWrite } from './useWrite';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Input } from '@/components/ui/input';

const DATA_TYPES: readonly CustomField['data_type'][] = [
  'date',
  'monetary',
  'integer',
  'text',
  'select',
];

/** optionsOf reads a choice's options from the box, one per line, the empty lines left out. */
function optionsOf(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '');
}

/**
 * The custom fields' page (spec decision 4, "the lists"): each field with its type of value in
 * words and a choice's options; a new field, an edit and a delete. A custom field holds no rule:
 * the extractor fills the ones it reads, and the operator the rest.
 */
export function CustomFieldsAdmin({
  fields,
  counts,
}: {
  readonly fields: readonly CustomField[];
  readonly counts?: Readonly<Record<string, number>>;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<CustomField | 'new' | null>(null);
  const [deleting, setDeleting] = useState<CustomField | null>(null);
  const [search, setSearch] = useState('');
  const visible = fields.filter((field) =>
    field.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()),
  );
  const remove = useWrite('documents.lists.failed');

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['documents-custom-fields'] });
  const typeWords = (field: CustomField) => {
    const type = t(`documents.lists.dataTypes.${field.data_type}`);
    return field.data_type === 'select' ? `${type}: ${(field.options ?? []).join(', ')}` : type;
  };
  const editor = (field?: CustomField) => (
    <FieldEditor
      field={field}
      onSaved={() => {
        setEditing(null);
        void refresh();
      }}
      onCancel={() => {
        setEditing(null);
      }}
    />
  );

  return (
    <div className="flex flex-col gap-3">
      <Input
        type="search"
        aria-label={t('documents.lists.search')}
        placeholder={t('documents.lists.search')}
        value={search}
        onChange={(event) => {
          setSearch(event.target.value);
        }}
      />
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          onClick={() => {
            setEditing('new');
          }}
        >
          {t('documents.lists.new.field')}
        </Button>
        {remove.error !== '' && (
          <p role="alert" className="text-sm text-danger">
            {remove.error}
          </p>
        )}
      </div>
      {editing === 'new' && editor()}
      {visible.length === 0 && editing !== 'new' && (
        <p className="text-sm text-text-muted">{t('documents.lists.empty')}</p>
      )}
      <ul className="flex flex-col">
        {visible.map((field) =>
          editing !== 'new' && editing?.id === field.id ? (
            <li key={field.id} className="py-3">
              {editor(field)}
            </li>
          ) : (
            <AdminEntry
              key={field.id}
              name={field.name}
              lines={[
                typeWords(field),
                ...(counts?.[field.id] === undefined
                  ? []
                  : [t('documents.lists.count', { count: counts[field.id] })]),
              ]}
              onEdit={() => {
                setEditing(field);
              }}
              onDelete={() => {
                setDeleting(field);
              }}
            />
          ),
        )}
      </ul>
      <DeleteConfirm
        name={deleting?.name ?? null}
        body={t('documents.lists.deleteFieldBody')}
        onCancel={() => {
          setDeleting(null);
        }}
        onConfirm={() => {
          const target = deleting;
          setDeleting(null);
          if (target === null) return;
          remove.run(async () => {
            await deleteCustomField(target.id);
            await refresh();
          });
        }}
      />
    </div>
  );
}

interface FieldEditorProps {
  readonly field: CustomField | undefined;
  readonly onSaved: () => void;
  readonly onCancel: () => void;
}

/** The editor of one custom field: its name, its type of value, and a choice's options, which
 * only a choice asks for. */
function FieldEditor({ field, onSaved, onCancel }: FieldEditorProps) {
  const { t } = useTranslation();
  const [name, setName] = useState(field?.name ?? '');
  const [dataType, setDataType] = useState<CustomField['data_type']>(field?.data_type ?? 'text');
  const [options, setOptions] = useState((field?.options ?? []).join('\n'));
  const { busy, error, run } = useWrite('documents.lists.failed');

  return (
    <EditorFrame
      title={
        field === undefined
          ? t('documents.lists.new.field')
          : t('documents.lists.edit', { name: field.name })
      }
      name={name}
      onNameChange={setName}
      busy={busy}
      error={error}
      onCancel={onCancel}
      onSave={() => {
        run(async () => {
          await saveCustomField(
            {
              name: name.trim(),
              data_type: dataType,
              ...(dataType === 'select' ? { options: optionsOf(options) } : {}),
            },
            field?.id,
          );
          onSaved();
        });
      }}
    >
      <KindChoice
        label={t('documents.lists.dataType')}
        kinds={DATA_TYPES}
        value={dataType}
        name={(type) => t(`documents.lists.dataTypes.${type}`)}
        onChange={setDataType}
        disabled={busy}
      />
      {dataType === 'select' && (
        <EditorField label={t('documents.lists.options')}>
          <Textarea
            value={options}
            disabled={busy}
            rows={4}
            onChange={(event) => {
              setOptions(event.target.value);
            }}
          />
        </EditorField>
      )}
    </EditorFrame>
  );
}

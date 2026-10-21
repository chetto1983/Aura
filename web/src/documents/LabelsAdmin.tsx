import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { AdminEntry, DeleteConfirm, EditorField, EditorFrame, KindChoice } from './adminParts';
import { deleteLabel, saveLabel, type Label, type LabelKind } from './documentsApi';
import { useWrite } from './useWrite';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';

/** The matching algorithms (internal/docfields/rules), in the order the editor offers them. */
const ALGORITHMS = ['none', 'any', 'all', 'exact', 'regex', 'fuzzy'] as const;
type Algorithm = (typeof ALGORITHMS)[number];

function algorithmOf(label: Pick<Label, 'match_algorithm'> | undefined): Algorithm {
  return ALGORITHMS.find((a) => a === label?.match_algorithm) ?? 'none';
}

/** ruleWords says a label's rule in words, as the list shows it. */
function ruleWords(label: Label, t: TFunction): string {
  const algorithm = algorithmOf(label);
  const words = t(`documents.lists.words.${algorithm}`, { pattern: label.match_pattern });
  if (algorithm === 'none' || label.case_insensitive) return words;
  return `${words} · ${t('documents.lists.caseSensitive')}`;
}

/** The queries a label's write changes: the lists, and the documents its rule suggested to. */
const invalidated = [['documents-labels'], ['documents']];

interface LabelsAdminProps {
  readonly kind: LabelKind;
  readonly labels: readonly Label[];
  /** How many documents of the library hold each label; undefined while they load. */
  readonly counts: Readonly<Record<string, number>> | undefined;
  readonly onFilter: (id: string) => void;
}

/**
 * One list's page (spec decision 4, "the lists"): each value with its rule in words, how many
 * documents hold it, and "Filtra documenti"; a new value, an edit and a delete. Saving a rule
 * runs it over the library, and the page says how many documents it suggested.
 */
export function LabelsAdmin({ kind, labels, counts, onFilter }: LabelsAdminProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<Label | 'new' | null>(null);
  const [deleting, setDeleting] = useState<Label | null>(null);
  const [status, setStatus] = useState('');
  const [search, setSearch] = useState('');
  const visible = labels.filter((label) =>
    label.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()),
  );
  const remove = useWrite('documents.lists.failed');

  const refresh = () =>
    Promise.all(invalidated.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
  const saved = (suggested: number) => {
    setEditing(null);
    setStatus(t('documents.lists.suggested', { count: suggested }));
    void refresh();
  };
  const editor = (label?: Label) => (
    <LabelEditor
      kind={kind}
      label={label}
      onSaved={saved}
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
            setStatus('');
          }}
        >
          {t(`documents.lists.new.${kind}`)}
        </Button>
        {status !== '' && (
          <p role="status" className="text-sm text-success">
            {status}
          </p>
        )}
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
        {visible.map((label) =>
          editing !== 'new' && editing?.id === label.id ? (
            <li key={label.id} className="py-3">
              {editor(label)}
            </li>
          ) : (
            <AdminEntry
              key={label.id}
              name={label.name}
              lines={[
                ruleWords(label, t),
                ...(counts === undefined
                  ? []
                  : [t('documents.lists.count', { count: counts[label.id] ?? 0 })]),
              ]}
              actions={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label={t('documents.lists.filterLabel', { name: label.name })}
                  onClick={() => {
                    onFilter(label.id);
                  }}
                >
                  {t('documents.lists.filter')}
                </Button>
              }
              onEdit={() => {
                setEditing(label);
                setStatus('');
              }}
              onDelete={() => {
                setDeleting(label);
              }}
            />
          ),
        )}
      </ul>
      <DeleteConfirm
        name={deleting?.name ?? null}
        body={t('documents.lists.deleteLabelBody')}
        onCancel={() => {
          setDeleting(null);
        }}
        onConfirm={() => {
          const target = deleting;
          setDeleting(null);
          if (target === null) return;
          remove.run(async () => {
            await deleteLabel(target.id);
            await refresh();
          });
        }}
      />
    </div>
  );
}

interface LabelEditorProps {
  readonly kind: LabelKind;
  readonly label: Label | undefined;
  readonly onSaved: (suggested: number) => void;
  readonly onCancel: () => void;
}

/** The editor of one label and its rule. The pattern box shows only for an algorithm that reads
 * one, and a rule is case-insensitive unless the operator says otherwise. */
function LabelEditor({ kind, label, onSaved, onCancel }: LabelEditorProps) {
  const { t } = useTranslation();
  const [name, setName] = useState(label?.name ?? '');
  const [algorithm, setAlgorithm] = useState<Algorithm>(algorithmOf(label));
  const [pattern, setPattern] = useState(label?.match_pattern ?? '');
  const [caseInsensitive, setCaseInsensitive] = useState(label?.case_insensitive ?? true);
  const { busy, error, run } = useWrite('documents.lists.failed');
  const reads = algorithm !== 'none';

  return (
    <EditorFrame
      title={
        label === undefined
          ? t(`documents.lists.new.${kind}`)
          : t('documents.lists.edit', { name: label.name })
      }
      name={name}
      onNameChange={setName}
      busy={busy}
      error={error}
      onCancel={onCancel}
      onSave={() => {
        run(async () => {
          const out = await saveLabel(
            {
              kind,
              name: name.trim(),
              match_algorithm: algorithm,
              match_pattern: reads ? pattern : '',
              case_insensitive: caseInsensitive,
            },
            label?.id,
          );
          onSaved(out.suggested);
        });
      }}
    >
      <KindChoice
        label={t('documents.lists.rule')}
        kinds={ALGORITHMS}
        value={algorithm}
        name={(a) => t(`documents.lists.algorithm.${a}`)}
        onChange={setAlgorithm}
        disabled={busy}
      />
      {reads && (
        <>
          <EditorField label={t('documents.lists.pattern')}>
            <Input
              value={pattern}
              disabled={busy}
              className="font-mono"
              onChange={(event) => {
                setPattern(event.target.value);
              }}
            />
          </EditorField>
          <label className="flex items-center gap-2 text-[13px] text-text">
            <Checkbox
              checked={caseInsensitive}
              disabled={busy}
              onCheckedChange={(checked) => {
                setCaseInsensitive(checked === true);
              }}
            />
            {t('documents.lists.caseInsensitive')}
          </label>
        </>
      )}
    </EditorFrame>
  );
}

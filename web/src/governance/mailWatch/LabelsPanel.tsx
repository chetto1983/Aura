import { useId, useState } from 'react';
import { Pencil, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { MailWatchLabel, MailWatchLabelInput } from '../mailWatchApi';
import { errorSentence, reservedLabelName, type TierChoice } from './mailWatchLogic';
import { PanelHeading, PanelPending, RemoveConfirm } from './PanelParts';
import { TierBadge, TierSelect } from './TierControls';
import { useAddLabel, useLabels, useRemoveLabel, useRules, useUpdateLabel } from './useMailWatch';
import { Spinner } from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// LabelsPanel is the person's words for what mail is about (spec §5.8). A label takes an optional
// tier and then acts as a rule. Removing one takes its rules with it, on the server, so the
// confirmation counts them first.

/** The labels table's CHECK, in characters. */
const LABEL_NAME_MAX = 60;
/** The labels table's CHECK on a description, in characters. */
const LABEL_DESCRIPTION_MAX = 300;

export function LabelsPanel() {
  const { t } = useTranslation();
  const labels = useLabels();
  const rules = useRules();
  const [editing, setEditing] = useState<string | null>(null);
  const [removing, setRemoving] = useState<MailWatchLabel | null>(null);
  const [addKey, setAddKey] = useState(0);
  const remove = useRemoveLabel(() => {
    setRemoving(null);
  });
  const add = useAddLabel(() => {
    setAddKey((key) => key + 1);
  });
  const ruleCount = (rules.data ?? []).filter((rule) => rule.label_id === removing?.id).length;

  return (
    <div className="flex flex-col gap-5 px-4 py-4">
      <PanelHeading
        title={t('governance.mailWatch.labels.heading')}
        intro={t('governance.mailWatch.labels.intro')}
      />
      {labels.isPending ? (
        <PanelPending />
      ) : labels.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.mailWatch.labels.loadError')}
        </p>
      ) : labels.data.length === 0 ? (
        <p className="text-[15px] text-text-muted">{t('governance.mailWatch.labels.empty')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {labels.data.map((label) =>
            editing === label.id ? (
              <LabelEditRow
                key={label.id}
                label={label}
                onDone={() => {
                  setEditing(null);
                }}
              />
            ) : (
              <LabelRow
                key={label.id}
                label={label}
                onEdit={() => {
                  setEditing(label.id);
                }}
                onRemove={() => {
                  setRemoving(label);
                }}
              />
            ),
          )}
        </ul>
      )}
      <LabelForm
        key={addKey}
        className="rounded-[var(--radius-sm)] border border-border bg-surface px-3 py-3"
        submitLabel={t('governance.mailWatch.labels.add')}
        pending={add.isPending}
        error={add.error}
        onSubmit={(input) => {
          add.mutate(input);
        }}
      />
      <RemoveConfirm
        open={removing !== null}
        title={t('governance.mailWatch.labels.removeTitle', { name: removing?.name ?? '' })}
        description={
          ruleCount === 0
            ? t('governance.mailWatch.labels.removeBodyNone')
            : t('governance.mailWatch.labels.removeBody', { count: ruleCount })
        }
        cancelLabel={t('governance.mailWatch.labels.removeCancel')}
        confirmLabel={t('governance.mailWatch.labels.removeConfirm')}
        pending={remove.isPending}
        error={remove.error}
        onCancel={() => {
          setRemoving(null);
          remove.reset();
        }}
        onConfirm={() => {
          if (removing !== null) remove.mutate(removing.id);
        }}
      />
    </div>
  );
}

interface LabelRowProps {
  readonly label: MailWatchLabel;
  readonly onEdit: () => void;
  readonly onRemove: () => void;
}

function LabelRow({ label, onEdit, onRemove }: LabelRowProps) {
  const { t } = useTranslation();
  const note = reservedLabelName(label.name)
    ? t('governance.mailWatch.decide.reserved')
    : label.description.trim() === ''
      ? t('governance.mailWatch.decide.noDescription')
      : null;
  return (
    <li className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-border bg-surface-2 py-1 pl-3 pr-1">
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="break-words text-[15px] text-text">{label.name}</span>
        {note === null ? null : <span className="text-[13px] text-text-muted">{note}</span>}
      </div>
      {label.discard ? (
        <Badge variant="secondary">{t('governance.mailWatch.decide.discard')}</Badge>
      ) : null}
      {label.tier === undefined ? null : <TierBadge tier={label.tier} />}
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t('governance.mailWatch.labels.rename', { name: label.name })}
        onClick={onEdit}
        className="text-text-muted hover:text-text"
      >
        <Pencil aria-hidden="true" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t('governance.mailWatch.labels.remove', { name: label.name })}
        onClick={onRemove}
        className="text-text-muted hover:text-text"
      >
        <Trash2 aria-hidden="true" />
      </Button>
    </li>
  );
}

function LabelEditRow({
  label,
  onDone,
}: {
  readonly label: MailWatchLabel;
  readonly onDone: () => void;
}) {
  const { t } = useTranslation();
  const update = useUpdateLabel(label.id, onDone);
  return (
    <li>
      <LabelForm
        className="rounded-[var(--radius-sm)] border border-border-strong bg-surface-2 px-3 py-3"
        initial={label}
        submitLabel={t('governance.mailWatch.labels.save')}
        pending={update.isPending}
        error={update.error}
        onSubmit={(input) => {
          update.mutate(input);
        }}
        onCancel={onDone}
      />
    </li>
  );
}

interface LabelFormProps {
  readonly className: string;
  readonly initial?: MailWatchLabel;
  readonly submitLabel: string;
  readonly pending: boolean;
  readonly error: Error | null;
  readonly onSubmit: (input: MailWatchLabelInput) => void;
  readonly onCancel?: () => void;
}

function LabelForm({
  className,
  initial,
  submitLabel,
  pending,
  error,
  onSubmit,
  onCancel,
}: LabelFormProps) {
  const { t } = useTranslation();
  const ids = {
    name: useId(),
    nameError: useId(),
    tier: useId(),
    description: useId(),
    descriptionHint: useId(),
  };
  const [name, setName] = useState(initial?.name ?? '');
  const [tier, setTier] = useState<TierChoice>(initial?.tier ?? '');
  const [description, setDescription] = useState(initial?.description ?? '');
  const [discard, setDiscard] = useState(initial?.discard ?? false);
  const [submitted, setSubmitted] = useState(false);
  const trimmed = name.trim();
  const nameInvalid = trimmed === '' || Array.from(trimmed).length > LABEL_NAME_MAX;
  const showNameError = submitted && nameInvalid;

  return (
    <form
      noValidate
      className={`flex flex-col gap-3 ${className}`}
      onSubmit={(event) => {
        event.preventDefault();
        setSubmitted(true);
        if (nameInvalid) return;
        onSubmit({ name: trimmed, tier, description: description.trim(), discard });
      }}
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-1">
          <Label htmlFor={ids.name}>{t('governance.mailWatch.labels.nameLabel')}</Label>
          <Input
            id={ids.name}
            value={name}
            aria-invalid={showNameError || undefined}
            aria-describedby={showNameError ? ids.nameError : undefined}
            onChange={(event) => {
              setName(event.currentTarget.value);
            }}
          />
          {showNameError ? (
            <p id={ids.nameError} className="text-[13px] text-danger">
              {t('governance.mailWatch.labels.nameInvalid')}
            </p>
          ) : null}
        </div>
        <div className="flex min-w-0 flex-col gap-1">
          <Label htmlFor={ids.tier}>{t('governance.mailWatch.labels.tierLabel')}</Label>
          <TierSelect id={ids.tier} value={tier} onChange={setTier} />
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <Label htmlFor={ids.description}>{t('governance.mailWatch.decide.description')}</Label>
        <Textarea
          id={ids.description}
          rows={2}
          maxLength={LABEL_DESCRIPTION_MAX}
          value={description}
          aria-describedby={ids.descriptionHint}
          onChange={(event) => {
            setDescription(event.currentTarget.value);
          }}
        />
        <p id={ids.descriptionHint} className="text-[13px] text-text-muted">
          {t('governance.mailWatch.decide.descriptionHint')}
        </p>
      </div>
      <DiscardSwitch value={discard} onChange={setDiscard} />
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={pending} aria-busy={pending || undefined}>
          {pending ? <Spinner /> : null}
          {submitLabel}
        </Button>
        {onCancel === undefined ? null : (
          <Button type="button" variant="ghost" onClick={onCancel}>
            {t('governance.mailWatch.labels.cancel')}
          </Button>
        )}
        {error === null ? null : (
          <p role="alert" className="min-w-0 text-[13px] text-danger">
            {errorSentence(error, t)}
          </p>
        )}
      </div>
    </form>
  );
}

/** Keep or discard, as one choice. Radix reports '' when the pressed side is pressed again, which
 * changes nothing: a label always does one or the other. */
function DiscardSwitch({
  value,
  onChange,
}: {
  readonly value: boolean;
  readonly onChange: (discard: boolean) => void;
}) {
  const { t } = useTranslation();
  const hint = useId();
  return (
    <div className="flex flex-wrap items-center gap-3">
      <ToggleGroup
        type="single"
        role="radiogroup"
        spacing={1}
        value={value ? 'discard' : 'keep'}
        aria-describedby={hint}
        onValueChange={(next: string) => {
          if (next !== '') onChange(next === 'discard');
        }}
        className="gap-0.5 rounded-[var(--radius-sm)] border border-border bg-surface p-0.5"
      >
        <ToggleGroupItem value="keep" className="h-9 px-3 text-[13px] font-semibold">
          {t('governance.mailWatch.decide.keep')}
        </ToggleGroupItem>
        <ToggleGroupItem value="discard" className="h-9 px-3 text-[13px] font-semibold">
          {t('governance.mailWatch.decide.discard')}
        </ToggleGroupItem>
      </ToggleGroup>
      <p id={hint} className="min-w-0 text-[13px] text-text-muted">
        {t('governance.mailWatch.decide.discardHint')}
      </p>
    </div>
  );
}

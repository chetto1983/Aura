import { useId, useState } from 'react';
import { AtSign, Globe, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { MailWatchLabel, MailWatchRule } from '../mailWatchApi';
import {
  draftIsComplete,
  errorSentence,
  parseRuleKey,
  previewRule,
  type RuleDraft,
  type TierChoice,
} from './mailWatchLogic';
import { PanelHeading, PanelPending, RemoveConfirm } from './PanelParts';
import { TierBadge, TierSelect } from './TierControls';
import {
  useAddRule,
  useDayPhrase,
  useDecisionsOfDay,
  useLabels,
  useMailWatchStatus,
  useRemoveRule,
  useRules,
} from './useMailWatch';
import { Spinner } from '@/components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';

// RulesPanel lists the identity's rules and writes new ones. A rule files a sender's mail from now
// on, so the form says what it would have done to the day before it is saved. That count is the
// client's own, over the decisions of every watched account, and the query behind it starts only
// once the draft is a rule: each read goes to the PIM live.

export function RulesPanel({ day }: { readonly day: string | null }) {
  const { t } = useTranslation();
  const rules = useRules();
  const labels = useLabels();
  const [removing, setRemoving] = useState<MailWatchRule | null>(null);
  const remove = useRemoveRule(() => {
    setRemoving(null);
  });
  const known = labels.data ?? [];
  return (
    <div className="flex flex-col gap-5 px-4 py-4">
      <PanelHeading
        title={t('governance.mailWatch.rules.heading')}
        intro={t('governance.mailWatch.rules.intro')}
      />
      <RuleForm rules={rules.data ?? []} labels={known} day={day} />
      {rules.isPending ? (
        <PanelPending />
      ) : rules.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.mailWatch.rules.loadError')}
        </p>
      ) : rules.data.length === 0 ? (
        <p className="text-[15px] text-text-muted">{t('governance.mailWatch.rules.empty')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rules.data.map((rule) => (
            <RuleRow key={rule.id} rule={rule} labels={known} onRemove={setRemoving} />
          ))}
        </ul>
      )}
      <RemoveConfirm
        open={removing !== null}
        title={t('governance.mailWatch.rules.removeTitle')}
        description={t('governance.mailWatch.rules.removeBody', { key: removing?.key ?? '' })}
        cancelLabel={t('governance.mailWatch.rules.removeCancel')}
        confirmLabel={t('governance.mailWatch.rules.removeConfirm')}
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

interface RuleRowProps {
  readonly rule: MailWatchRule;
  readonly labels: readonly MailWatchLabel[];
  readonly onRemove: (rule: MailWatchRule) => void;
}

function RuleRow({ rule, labels, onRemove }: RuleRowProps) {
  const { t } = useTranslation();
  const labelName = labels.find((label) => label.id === rule.label_id)?.name;
  const Kind = rule.key_kind === 'domain' ? Globe : AtSign;
  return (
    <li className="flex flex-wrap items-center gap-x-3 rounded-[var(--radius-sm)] border border-border bg-surface-2 py-1 pl-3 pr-1">
      <Kind aria-hidden="true" className="size-4 shrink-0 text-text-muted" />
      <div className="min-w-0 flex-[1_1_14rem]">
        <p className="break-all font-mono text-[13px] text-text">
          <bdi>{rule.key}</bdi>
        </p>
        <p className="text-[13px] text-text-muted">
          {t(`governance.mailWatch.rules.kind.${rule.key_kind}`)} ·{' '}
          {t(`governance.mailWatch.rules.source.${rule.source}`)}
        </p>
      </div>
      <div className="flex flex-wrap justify-end gap-1.5">
        {rule.tier === undefined ? null : <TierBadge tier={rule.tier} />}
        {labelName === undefined ? null : <Badge variant="secondary">{labelName}</Badge>}
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t('governance.mailWatch.rules.remove', { key: rule.key })}
        onClick={() => {
          onRemove(rule);
        }}
        className="ml-auto text-text-muted hover:text-text"
      >
        <Trash2 aria-hidden="true" />
      </Button>
    </li>
  );
}

interface RuleFormProps {
  readonly rules: readonly MailWatchRule[];
  readonly labels: readonly MailWatchLabel[];
  readonly day: string | null;
}

function RuleForm({ rules, labels, day }: RuleFormProps) {
  const { t } = useTranslation();
  const ids = { key: useId(), keyError: useId(), tier: useId(), label: useId() };
  const [key, setKey] = useState('');
  const [tier, setTier] = useState<TierChoice>('');
  const [labelId, setLabelId] = useState('');
  const [submitted, setSubmitted] = useState(false);

  const draft = { key, tier, labelId };
  const complete = draftIsComplete(draft);
  const keyInvalid = parseRuleKey(key) === null;
  const add = useAddRule(() => {
    setKey('');
    setTier('');
    setLabelId('');
    setSubmitted(false);
  });

  return (
    <form
      noValidate
      className="flex flex-col gap-3 rounded-[var(--radius-sm)] border border-border bg-surface px-3 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        setSubmitted(true);
        if (!complete) return;
        add.mutate({
          key: key.trim(),
          ...(tier === '' ? {} : { tier }),
          ...(labelId === '' ? {} : { label_id: labelId }),
        });
      }}
    >
      <div className="flex flex-col gap-1">
        <Label htmlFor={ids.key}>{t('governance.mailWatch.rules.keyLabel')}</Label>
        <Input
          id={ids.key}
          value={key}
          placeholder={t('governance.mailWatch.rules.keyPlaceholder')}
          autoComplete="off"
          spellCheck={false}
          aria-invalid={(submitted && keyInvalid) || undefined}
          aria-describedby={submitted && keyInvalid ? ids.keyError : undefined}
          onChange={(event) => {
            setKey(event.currentTarget.value);
          }}
        />
        {submitted && keyInvalid ? (
          <p id={ids.keyError} className="text-[13px] text-danger">
            {t('governance.mailWatch.rules.keyInvalid')}
          </p>
        ) : null}
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-1">
          <Label htmlFor={ids.tier}>{t('governance.mailWatch.rules.tierLabel')}</Label>
          <TierSelect id={ids.tier} value={tier} onChange={setTier} />
        </div>
        <div className="flex min-w-0 flex-col gap-1">
          <Label htmlFor={ids.label}>{t('governance.mailWatch.rules.labelLabel')}</Label>
          <Select
            id={ids.label}
            value={labelId}
            onValueChange={setLabelId}
            options={[
              { value: '', label: t('governance.mailWatch.rules.noneLabel') },
              ...labels.map((label) => ({ value: label.id, label: label.name })),
            ]}
          />
        </div>
      </div>
      {submitted && !keyInvalid && !complete ? (
        <p className="text-[13px] text-danger">{t('governance.mailWatch.rules.needsEffect')}</p>
      ) : null}
      <RulePreview draft={draft} complete={complete} rules={rules} labels={labels} day={day} />
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={add.isPending} aria-busy={add.isPending || undefined}>
          {add.isPending ? <Spinner /> : null}
          {t('governance.mailWatch.rules.add')}
        </Button>
        {add.isError ? (
          <p role="alert" className="min-w-0 text-[13px] text-danger">
            {errorSentence(add.error, t)}
          </p>
        ) : null}
      </div>
    </form>
  );
}

interface RulePreviewProps {
  readonly draft: RuleDraft;
  readonly complete: boolean;
  readonly rules: readonly MailWatchRule[];
  readonly labels: readonly MailWatchLabel[];
  readonly day: string | null;
}

function RulePreview({ draft, complete, rules, labels, day }: RulePreviewProps) {
  const { t } = useTranslation();
  const status = useMailWatchStatus();
  const accountIds = complete ? (status.data?.accounts ?? []).map((a) => a.account_id) : [];
  const { ready, decisions } = useDecisionsOfDay(accountIds, day);
  const when = useDayPhrase(day);
  const count = ready ? previewRule(draft, decisions, rules, labels) : null;
  return (
    <p role="status" className="text-[13px] text-text-muted empty:hidden">
      {count === null
        ? null
        : count === 0
          ? t('governance.mailWatch.rules.previewNone', { when })
          : t('governance.mailWatch.rules.previewChanges', { count, when })}
    </p>
  );
}

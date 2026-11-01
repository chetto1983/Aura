import { Check, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { TierBadge } from './TierControls';
import { useUndoRule, type RuleChange } from './useMailWatch';
import { Spinner } from '@/components/Spinner';
import { Button } from '@/components/ui/button';

// DecisionNotice says what a correction just did. A rule learned is said out loud, with an Undo
// that stays until the person acts or replaces it: a timed undo would punish a slow reader, and
// the moment the watch starts acting on its own is the moment to be in plain sight. The live
// region stays mounted so a screen reader hears each change; a failure speaks as an alert.

export type Notice =
  | { readonly kind: 'rule'; readonly learned: boolean; readonly change: RuleChange }
  | { readonly kind: 'undone' }
  | { readonly kind: 'problem'; readonly text: string };

interface DecisionNoticeProps {
  readonly notice: Notice | null;
  readonly onChange: (notice: Notice | null) => void;
}

export function DecisionNotice({ notice, onChange }: DecisionNoticeProps) {
  const { t } = useTranslation();
  return (
    <>
      <div role="status" className="mx-4 my-2 empty:hidden">
        {notice?.kind === 'rule' ? (
          <RuleNotice
            key={notice.change.rule.id}
            learned={notice.learned}
            change={notice.change}
            onChange={onChange}
          />
        ) : null}
        {notice?.kind === 'undone' ? (
          <p className="text-[13px] text-text-muted">{t('governance.mailWatch.notice.undone')}</p>
        ) : null}
      </div>
      {notice?.kind === 'problem' ? (
        <p role="alert" className="mx-4 my-2 text-[13px] text-danger">
          {notice.text}
        </p>
      ) : null}
    </>
  );
}

interface RuleNoticeProps {
  readonly learned: boolean;
  readonly change: RuleChange;
  readonly onChange: (notice: Notice | null) => void;
}

function RuleNotice({ learned, change, onChange }: RuleNoticeProps) {
  const { t } = useTranslation();
  const undo = useUndoRule(() => {
    onChange({ kind: 'undone' });
  });
  const { rule } = change;
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-[var(--radius-sm)] border border-border-strong bg-surface-3 py-1 pl-3 pr-1 shadow-popover motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-bottom-2">
      <Check aria-hidden="true" className="size-4 shrink-0 text-success" />
      <span className="text-[13px] font-semibold text-text">
        {t(learned ? 'governance.mailWatch.notice.learned' : 'governance.mailWatch.notice.saved')}
      </span>
      <span className="flex min-w-0 items-center gap-2">
        <bdi className="block min-w-0 truncate font-mono text-[12px] text-text-muted">
          {rule.key}
        </bdi>
        {rule.tier === undefined ? null : <TierBadge tier={rule.tier} />}
      </span>
      <span className="ml-auto flex items-center">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={undo.isPending}
          aria-busy={undo.isPending || undefined}
          onClick={() => {
            undo.mutate(change);
          }}
        >
          {undo.isPending ? <Spinner /> : null}
          {t('governance.mailWatch.notice.undo')}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t('governance.mailWatch.notice.dismiss')}
          onClick={() => {
            onChange(null);
          }}
          className="text-text-muted"
        >
          <X aria-hidden="true" />
        </Button>
      </span>
      {undo.isError ? (
        <p role="alert" className="w-full pb-1 text-[13px] text-danger">
          {t('governance.mailWatch.notice.undoFailed')}
        </p>
      ) : null}
    </div>
  );
}

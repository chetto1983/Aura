import { useRef, useState, type KeyboardEvent } from 'react';
import { CalendarDays, Sparkles, TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { MailWatchDecision, MailWatchLabel, MailWatchTier } from '../mailWatchApi';
import { dayAndTime, rowTime, triageAction, whySentence } from './mailWatchLogic';
import { TierBadge, TierControl, TierRail } from './TierControls';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { cn } from '@/lib/utils';

// DecisionRow is one verdict: who, what, why, and the controls to change it. The sender and the
// subject are a stranger's text, so they stay plain React text, wrapped in <bdi> so a right-to-left
// override in a subject cannot run on into the address beside it. A display name is whatever the
// sender typed, so the address always shows beside it and wraps rather than truncates: the domain
// comes last, and it is the part that cannot be forged.
//
// The row's text is a button that opens it to the whole sender and subject, because a clamped
// subject has no other way to be read on a touch screen. That button is the row's stop in the
// roving tabindex, it takes the triage keys, and only the active row's controls are in the tab
// order. The label menu sits outside it, so a key in the open menu never reaches the row, and when
// the menu closes the focus comes back to the button, where J and K work, not to the select.

export interface DecisionChange {
  readonly tier?: MailWatchTier;
  readonly labelId?: string;
}

/** The "from now on, always like this" action, offered on the active row when it means anything. */
export interface AlwaysOffer {
  readonly preview: string | null;
  readonly pending: boolean;
  readonly onAccept: () => void;
}

interface DecisionRowProps {
  readonly row: MailWatchDecision;
  readonly labels: readonly MailWatchLabel[];
  readonly active: boolean;
  readonly busy: boolean;
  readonly always: AlwaysOffer | null;
  readonly onActivate: () => void;
  readonly onMove: (delta: 1 | -1) => void;
  readonly onCorrect: (change: DecisionChange) => void;
}

export function DecisionRow({
  row,
  labels,
  active,
  busy,
  always,
  onActivate,
  onMove,
  onCorrect,
}: DecisionRowProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const [labelOpen, setLabelOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);

  function onKeyDown(event: KeyboardEvent<HTMLElement>) {
    const action = triageAction(event);
    if (action === null) return;
    event.preventDefault();
    if (action.kind === 'move') onMove(action.delta);
    else if (!row.gone && action.kind === 'label') setLabelOpen(true);
    else if (!row.gone && action.kind === 'tier' && action.tier !== row.tier) {
      onCorrect({ tier: action.tier });
    }
  }

  return (
    <li
      data-active={active}
      className="relative grid scroll-mb-14 scroll-mt-10 grid-cols-1 gap-x-4 gap-y-2 rounded-[var(--radius-sm)] py-2.5 pl-4 pr-2 transition-colors hover:bg-surface-2/60 has-[[data-mail-watch-row]:focus-visible]:ring-2 has-[[data-mail-watch-row]:focus-visible]:ring-ring data-[active=true]:bg-surface-2 @lg/day:grid-cols-[minmax(0,1fr)_auto] @lg/day:items-center"
    >
      <TierRail tier={row.tier} />
      <button
        ref={button}
        type="button"
        data-mail-watch-row
        tabIndex={active ? 0 : -1}
        aria-expanded={expanded}
        aria-busy={busy || undefined}
        aria-keyshortcuts="1 2 3 J K L"
        onFocus={onActivate}
        onClick={() => {
          onActivate();
          setExpanded((open) => !open);
        }}
        onKeyDown={onKeyDown}
        className="block min-w-0 cursor-default text-left outline-none"
      >
        {row.gone ? <GoneText row={row} /> : <MailText row={row} expanded={expanded} />}
        <RowReason row={row} expanded={expanded} />
      </button>

      <div className="flex min-w-0 flex-col gap-1 @lg/day:items-end">
        {row.gone ? (
          <TierBadge tier={row.tier} />
        ) : (
          <div className="flex min-w-0 items-center gap-2">
            <TierControl
              value={row.tier}
              tabbable={active}
              onChange={(tier) => {
                onCorrect({ tier });
              }}
              onFocus={onActivate}
              onKeyDown={onKeyDown}
            />
            {labels.length > 0 ? (
              <Select
                size="sm"
                open={labelOpen}
                onOpenChange={(open) => {
                  setLabelOpen(open);
                  if (open) onActivate();
                }}
                onCloseAutoFocus={(event) => {
                  event.preventDefault();
                  button.current?.focus({ preventScroll: true });
                }}
                aria-label={t('governance.mailWatch.day.labelAria')}
                value={row.label_id ?? ''}
                onValueChange={(labelId) => {
                  onCorrect({ labelId });
                }}
                options={[
                  { value: '', label: t('governance.mailWatch.day.unsorted') },
                  ...labels.map((label) => ({ value: label.id, label: label.name })),
                ]}
                className="min-h-11 w-full min-w-0 max-w-44 @lg/day:min-h-10 @lg/day:w-36"
                {...(active ? {} : { tabIndex: -1 })}
              />
            ) : null}
          </div>
        )}
        {/* In the controls' column, where a three-line row already has the height: anywhere
            else it would add a line to the active row and shift the list on every J and K. */}
        {always === null ? null : (
          <>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={always.pending}
              aria-busy={always.pending || undefined}
              onClick={always.onAccept}
              className="-ml-3 text-accent-text pointer-fine:min-h-8 @lg/day:-mr-1 @lg/day:ml-0"
            >
              <Sparkles aria-hidden="true" />
              {t('governance.mailWatch.row.always')}
            </Button>
            {always.preview === null ? null : (
              <span className="text-[13px] text-text-muted">{always.preview}</span>
            )}
          </>
        )}
      </div>
    </li>
  );
}

function GoneText({ row }: { readonly row: MailWatchDecision }) {
  const { t } = useTranslation();
  return (
    <span className="block text-[15px] italic text-text-muted">
      {row.resource === 'calendar' ? (
        <span className="sr-only">{t('governance.mailWatch.row.event')}: </span>
      ) : null}
      {t('governance.mailWatch.row.gone')}
    </span>
  );
}

function MailText({
  row,
  expanded,
}: {
  readonly row: MailWatchDecision;
  readonly expanded: boolean;
}) {
  const { t } = useTranslation();
  const sender = row.from_name !== '' ? row.from_name : row.from;
  return (
    <>
      <span className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        {row.resource === 'calendar' ? (
          <CalendarDays aria-hidden="true" className="size-3.5 self-center text-text-muted" />
        ) : null}
        <bdi
          title={sender}
          className={cn(
            'block max-w-full text-[15px] font-semibold text-text',
            expanded ? 'break-words' : 'truncate',
          )}
        >
          {sender === '' ? t('governance.mailWatch.row.unknownSender') : sender}
        </bdi>
        {row.from_name !== '' ? (
          <bdi className="block max-w-full break-all font-mono text-[12px] text-text-muted">
            {row.from}
          </bdi>
        ) : null}
      </span>
      <span
        className={cn(
          'block break-words text-[15px] leading-snug',
          !expanded && 'line-clamp-2',
          row.subject === '' ? 'italic text-text-muted' : 'text-text',
        )}
      >
        <bdi>{row.subject === '' ? t('governance.mailWatch.row.noSubject') : row.subject}</bdi>
      </span>
    </>
  );
}

function RowReason({
  row,
  expanded,
}: {
  readonly row: MailWatchDecision;
  readonly expanded: boolean;
}) {
  const { t, i18n } = useTranslation();
  return (
    <>
      <span className="mt-0.5 block text-[13px] text-text-muted">
        <time dateTime={row.start ?? row.decided_at} className="mr-2 font-mono text-[12px]">
          {rowTime(row, i18n.language)}
        </time>
        {whySentence(row.decided_by, t)}
      </span>
      {row.auth_failed ? (
        <Badge variant="warning" className="mt-1">
          <TriangleAlert aria-hidden="true" />
          {t('governance.mailWatch.row.unverified')}
        </Badge>
      ) : null}
      {expanded ? (
        <span className="block text-[13px] text-text-muted">
          {t('governance.mailWatch.row.decidedAt', {
            time: dayAndTime(row.decided_at, i18n.language),
          })}
        </span>
      ) : null}
    </>
  );
}

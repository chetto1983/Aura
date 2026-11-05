import { useState } from 'react';
import { Inbox } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { DayHeader } from './DayHeader';
import { DecisionList } from './DecisionList';
import { DecisionNotice, type Notice } from './DecisionNotice';
import { errorSentence } from './mailWatchLogic';
import {
  useAddRule,
  useCorrection,
  useCorrectionsInFlight,
  useDayPhrase,
  useDecisions,
  useLabels,
  useRules,
} from './useMailWatch';
import { SkeletonBlock } from '@/components/skeleton';
import { Button } from '@/components/ui/button';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
import { Kbd } from '@/components/ui/kbd';

// DecisionsView is one account's one day: what Aura decided, why, and the place to change it. A
// correction is optimistic, because triage by key cannot wait for the server's live read of the
// sender, and a refused one puts the row back and says so.

/** The server reads at most this many verdicts of a day (decisionsLimit in service.go). */
const DAY_LIMIT = 200;

interface DecisionsViewProps {
  readonly accountId: string;
  readonly day: string | null;
  readonly onDayChange: (day: string | null) => void;
  readonly claimFocus: () => boolean;
}

export function DecisionsView({ accountId, day, onDayChange, claimFocus }: DecisionsViewProps) {
  const { t } = useTranslation();
  const [showDiscarded, setShowDiscarded] = useState(false);
  const decisions = useDecisions(accountId, day, showDiscarded);
  const labels = useLabels();
  const rules = useRules();
  const [notice, setNotice] = useState<Notice | null>(null);
  const correct = useCorrection(accountId, day, {
    onLearned: (change, created) => {
      setNotice({ kind: 'rule', learned: true, undoable: created, change });
    },
    onFailed: () => {
      setNotice({ kind: 'problem', text: t('governance.mailWatch.notice.correctionFailed') });
    },
  });
  const inFlight = useCorrectionsInFlight();
  const addRule = useAddRule((change, created) => {
    setNotice({ kind: 'rule', learned: false, undoable: created, change });
  });
  const when = useDayPhrase(day);
  const rows = decisions.data?.decisions ?? [];
  const discardedCount = decisions.data?.discarded ?? 0;

  return (
    <div className="@container/day flex min-h-full flex-col">
      <DayHeader day={day ?? decisions.data?.day ?? null} rows={rows} onDayChange={onDayChange} />
      {discardedCount > 0 || showDiscarded ? (
        <div className="px-4 pb-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-pressed={showDiscarded}
            onClick={() => {
              setShowDiscarded((shown) => !shown);
            }}
          >
            {t('governance.mailWatch.decide.decisions.discarded')} ({discardedCount})
          </Button>
        </div>
      ) : null}
      <div className="flex-1">
        {decisions.isPending || labels.isPending ? (
          <DayPending />
        ) : decisions.isError ? (
          <div className="flex flex-col items-start gap-2 px-4 py-3">
            <p role="alert" className="text-[15px] text-danger">
              {errorSentence(decisions.error, t)}
            </p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                void decisions.refetch();
              }}
            >
              {t('governance.mailWatch.day.retry')}
            </Button>
          </div>
        ) : rows.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Inbox aria-hidden="true" />
              </EmptyMedia>
              <EmptyTitle className="font-display text-[20px]">
                {t('governance.mailWatch.day.emptyHeading')}
              </EmptyTitle>
              <EmptyDescription>{t('governance.mailWatch.day.emptyBody')}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <DecisionList
            rows={rows}
            labels={labels.data ?? []}
            rules={rules.data ?? []}
            inFlight={inFlight}
            savingRule={addRule.isPending}
            when={when}
            claimFocus={claimFocus}
            onCorrect={(row, change) => {
              setNotice(null);
              correct.mutate({ row, ...change });
            }}
            onAlways={(input) => {
              setNotice(null);
              addRule.mutate(input, {
                onError: (error) => {
                  setNotice({ kind: 'problem', text: errorSentence(error, t) });
                },
              });
            }}
          />
        )}
        {rows.length >= DAY_LIMIT ? (
          <p className="px-4 pb-3 text-[13px] text-text-muted">
            {t('governance.mailWatch.day.truncated', { count: DAY_LIMIT })}
          </p>
        ) : null}
      </div>
      <div className="sticky bottom-0 z-[2] bg-surface">
        <DecisionNotice notice={notice} onChange={setNotice} />
        {rows.length > 0 ? <KeysHint /> : null}
      </div>
    </div>
  );
}

function DayPending() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 px-4 pb-4">
      <p role="status" className="sr-only">
        {t('governance.mailWatch.day.loading')}
      </p>
      {[0, 1, 2, 3].map((row) => (
        <SkeletonBlock key={row} height={72} />
      ))}
    </div>
  );
}

/** Discreet, and only where keys exist: a touch screen has none to press. */
function KeysHint() {
  const { t } = useTranslation();
  return (
    <p className="hidden flex-wrap items-center gap-x-4 gap-y-1 border-t border-border px-4 py-2 text-[12px] text-text-muted pointer-fine:flex">
      <span className="inline-flex items-center gap-1">
        <Kbd>J</Kbd>
        <Kbd>K</Kbd>
        {t('governance.mailWatch.keys.move')}
      </span>
      <span className="inline-flex items-center gap-1">
        <Kbd>1</Kbd>
        <Kbd>2</Kbd>
        <Kbd>3</Kbd>
        {t('governance.mailWatch.keys.tier')}
      </span>
      <span className="inline-flex items-center gap-1">
        <Kbd>L</Kbd>
        {t('governance.mailWatch.keys.label')}
      </span>
    </p>
  );
}

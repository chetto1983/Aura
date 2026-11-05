import { useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type {
  MailWatchDecision,
  MailWatchLabel,
  MailWatchRule,
  MailWatchRuleInput,
} from '../mailWatchApi';
import { DecisionRow, type AlwaysOffer, type DecisionChange } from './DecisionRow';
import {
  canTakeRule,
  decisionKey,
  groupDecisions,
  isTypingTarget,
  previewRule,
  ruleInputFrom,
  ruleOnFile,
  type DecisionGroup,
  type RuleDraft,
} from './mailWatchLogic';
import { TierMark } from './TierControls';

// DecisionList draws the day's groups and owns the roving tabindex across them. One rule keeps the
// focus sane: while the list holds the focus, the focus sits in the active row. That one rule is
// J and K, and it is also what happens when a correction sends the focused row to another group:
// the row remounts elsewhere, the active index stays where the person was, and the next mail takes
// the focus, which is what triage wants.

const ACTIVE_ITEM = 'li[data-active="true"]';

interface DecisionListProps {
  readonly rows: readonly MailWatchDecision[];
  readonly labels: readonly MailWatchLabel[];
  readonly rules: readonly MailWatchRule[];
  readonly inFlight: ReadonlySet<string>;
  /** Rule writes in flight, so a row's one-click action cannot fire twice. */
  readonly savingRule: boolean;
  /** The day as a phrase for a preview: "today", "on 28 October". */
  readonly when: string;
  /** True the first time it is asked since the dialog opened: the list takes the focus once. */
  readonly claimFocus: () => boolean;
  readonly onCorrect: (row: MailWatchDecision, change: DecisionChange) => void;
  readonly onAlways: (input: MailWatchRuleInput) => void;
}

function hasThisRule(rules: readonly MailWatchRule[], row: MailWatchDecision): boolean {
  const onFile = ruleOnFile(rules, { key: row.from.toLowerCase(), key_kind: 'sender' });
  return onFile?.tier === row.tier && onFile.label_id === row.label_id;
}

export function DecisionList({
  rows,
  labels,
  rules,
  inFlight,
  savingRule,
  when,
  claimFocus,
  onCorrect,
  onAlways,
}: DecisionListProps) {
  const { t } = useTranslation();
  const groups = groupDecisions(rows, labels);
  const ordered = groups.flatMap((group) => group.rows);
  const indexOf = new Map(ordered.map((row, index) => [decisionKey(row), index]));

  const root = useRef<HTMLDivElement>(null);
  const ownsFocus = useRef(false);
  const [cursor, setCursor] = useState(0);
  const active = Math.min(cursor, ordered.length - 1);

  useLayoutEffect(() => {
    if (claimFocus()) ownsFocus.current = !isTypingTarget(document.activeElement);
    if (!ownsFocus.current) return;
    const item = root.current?.querySelector<HTMLElement>(ACTIVE_ITEM);
    if (item == null || item.contains(document.activeElement)) return;
    // focus() alone leaves the row under the dialog's sticky footer: only scrollIntoView honours
    // the row's scroll margins.
    item.querySelector<HTMLElement>('[data-mail-watch-row]')?.focus({ preventScroll: true });
    item.scrollIntoView({ block: 'nearest' });
  });

  function offerFor(row: MailWatchDecision): AlwaysOffer | null {
    if (!canTakeRule(row) || hasThisRule(rules, row)) return null;
    const input = ruleInputFrom(row);
    const draft: RuleDraft = {
      key: input.key,
      tier: input.tier ?? '',
      labelId: input.label_id ?? '',
    };
    const count = previewRule(draft, rows, rules, labels);
    return {
      preview: count === 0 ? null : t('governance.mailWatch.row.alwaysPreview', { count, when }),
      pending: savingRule,
      onAccept: () => {
        onAlways(ruleInputFrom(row));
      },
    };
  }

  return (
    <div
      ref={root}
      className="flex flex-col gap-4 px-2 pb-4"
      onFocus={() => {
        ownsFocus.current = true;
      }}
      onBlur={(event) => {
        const next = event.relatedTarget;
        if (next instanceof Node && !event.currentTarget.contains(next)) ownsFocus.current = false;
      }}
    >
      {groups.map((group) => (
        <DecisionGroupSection key={group.id} group={group}>
          {group.rows.map((row) => {
            const key = decisionKey(row);
            const index = indexOf.get(key) ?? 0;
            const isActive = index === active;
            return (
              <DecisionRow
                key={key}
                row={row}
                labels={labels}
                active={isActive}
                busy={inFlight.has(key)}
                always={isActive ? offerFor(row) : null}
                onActivate={() => {
                  setCursor(index);
                }}
                onMove={(delta) => {
                  setCursor(Math.max(0, Math.min(ordered.length - 1, active + delta)));
                }}
                onCorrect={(change) => {
                  if (!inFlight.has(key)) onCorrect(row, change);
                }}
              />
            );
          })}
        </DecisionGroupSection>
      ))}
    </div>
  );
}

function DecisionGroupSection({
  group,
  children,
}: {
  readonly group: DecisionGroup;
  readonly children: ReactNode;
}) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <section aria-labelledby={id}>
      <h4
        id={id}
        className="sticky top-0 z-[1] flex items-baseline justify-between bg-surface px-2 py-1.5 text-[13px] font-semibold uppercase tracking-wide text-text-muted"
      >
        {group.kind === 'now' ? (
          <TierMark tier="now">{t('governance.mailWatch.tier.now')}</TierMark>
        ) : (
          <span className="min-w-0 truncate">
            {group.kind === 'label' ? group.name : t('governance.mailWatch.day.unsorted')}
          </span>
        )}
        <span className="font-mono text-[12px] font-normal">{group.rows.length}</span>
      </h4>
      <ul className="flex flex-col gap-0.5">{children}</ul>
    </section>
  );
}

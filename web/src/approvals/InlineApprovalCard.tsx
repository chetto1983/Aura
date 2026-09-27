import { useId, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MessageSquareText, ShieldCheck, TriangleAlert } from 'lucide-react';
import { ariaInvalid } from '../a11y/aria';
import { CancelControl } from '../questions/CancelControl';
import { QuestionCard } from '../questions/QuestionCard';
import { QuestionOptions } from '../questions/QuestionOptions';
import { QuestionReceipt, type ReceiptTone } from '../questions/QuestionReceipt';
import { approvalQuestion } from './approvalQuestion';
import {
  isDestructiveApproval,
  isTerminal,
  offersOnlyScopes,
  parseOptions,
  parseScopeChoice,
  type ApprovalOption,
} from './approvalState';
import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';
import {
  useResolveApproval,
  type Approval,
  type ResolveAction,
  type ResolveOutcome,
} from './useApprovals';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

// InlineApprovalCard is ask_user's adapter onto QuestionCard (spec 2026-09-25). Options are
// radio rows and a pill that stays grey until one is chosen; with no options the reply is free
// text, for every kind, as before. The pill says Approve only when every option is a gateway
// scope. Recognized approval metadata is rendered in the active locale; legacy/unknown questions
// remain escaped React text, and the URL capability token never appears as a visible field.

type CardState = 'pending' | 'answered' | 'declined' | 'cancelled';

interface Receipt {
  readonly tone: ReceiptTone;
  readonly key: string;
}

const RECEIPTS: Record<Exclude<CardState, 'pending'>, Receipt> = {
  answered: { tone: 'success', key: 'approval.card.answered' },
  declined: { tone: 'neutral', key: 'approval.card.declined' },
  cancelled: { tone: 'danger', key: 'approval.card.cancelled' },
};

/**
 * useScopeLabel renders a gateway approval-scope option in the operator's language. The
 * gateway ships a stable code plus an English fallback label; a scope it does not
 * recognise falls back to that label rather than rendering a raw code.
 */
function useScopeLabel(): (option: ApprovalOption) => string {
  const { t } = useTranslation();
  return (option) => {
    const choice = parseScopeChoice(option.value);
    if (choice === null) return option.label;
    return t(`approval.scope.${choice.scope}`, { subject: choice.subject });
  };
}

/**
 * cardStateFor maps the server's verdict to the receipt. The server is authoritative — a
 * scheduled gate reports approved/rejected regardless of which button produced it — and the
 * action is only the fallback for the in-session verdicts ('continue'/'pending'), where the
 * receipt reflects what the operator just did while the turn goes on.
 */
function cardStateFor(outcome: ResolveOutcome, action: ResolveAction): CardState {
  switch (outcome) {
    case 'approved':
      return 'answered';
    case 'rejected':
      return 'declined';
    case 'terminated':
      return 'cancelled';
    default:
      return action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled';
  }
}

export interface InlineApprovalCardProps {
  readonly approval: Approval;
  /** True while the owning run is actively streaming; gates cancel confirmation. */
  readonly isStreaming?: boolean;
  /** Notify the shared thread gate after a successful local resolution. */
  readonly onResolved?: (resolution: ApprovalResolution) => void | Promise<void>;
  /** Notify the gate before the resolve request starts so Cancel owns the generation. */
  readonly onResolutionStarted?: (attempt: ApprovalResolutionAttempt) => void;
  /** Release a matching lifecycle attempt if its resolve request fails. */
  readonly onResolutionFailed?: (attempt: ApprovalResolutionAttempt) => void | Promise<void>;
}

export function InlineApprovalCard({
  approval,
  isStreaming,
  onResolved,
  onResolutionStarted,
  onResolutionFailed,
}: InlineApprovalCardProps) {
  const { t } = useTranslation();
  const resolve = useResolveApproval();
  const options = parseOptions(approval.options);
  const scopeLabel = useScopeLabel();
  const [freeText, setFreeText] = useState('');
  // An index, not a value: nothing makes two options' values distinct.
  const [chosen, setChosen] = useState<number | null>(null);
  const [state, setState] = useState<CardState>('pending');
  const [given, setGiven] = useState('');
  const attemptSequence = useRef(0);
  const baseId = useId();
  const busy = resolve.isPending;
  const failed = resolve.isError;
  const isApproval = approval.kind === 'approval';
  const destructive = isApproval && isDestructiveApproval(approval);
  const choosing = options.length > 0;
  const chosenOption = chosen === null ? undefined : options[chosen];
  const verb =
    isApproval && offersOnlyScopes(options) ? 'questionCard.approve' : 'approval.card.answer';

  function submit(action: ResolveAction, content?: string, shown = '') {
    attemptSequence.current += 1;
    const attempt: ApprovalResolution = {
      approval,
      action,
      attemptId: `${baseId}:${String(attemptSequence.current)}`,
    };
    onResolutionStarted?.(attempt);
    resolve.mutate(
      action === 'accept'
        ? { token: approval.token, action, content: content ?? '' }
        : { token: approval.token, action },
      {
        onSuccess: (directive) => {
          setGiven(shown);
          setState(cardStateFor(directive.outcome, action));
          // The verdict rides along so the thread gate re-drives the turn only when the model
          // actually has more work ('continue'); a scheduled gate is already complete.
          void onResolved?.({ ...attempt, outcome: directive.outcome });
        },
        onError: () => {
          void onResolutionFailed?.(attempt);
        },
      },
    );
  }

  function answer() {
    if (!choosing) submit('accept', freeText, freeText.trim());
    else if (chosenOption !== undefined) {
      submit('accept', chosenOption.value, scopeLabel(chosenOption));
    }
  }

  function frame(children: ReactNode, footer?: ReactNode) {
    return (
      <QuestionCard
        titleId={`${baseId}-title`}
        descriptionId={`${baseId}-question`}
        icon={<FrameIcon approval={isApproval} destructive={destructive} />}
        title={t(isApproval ? 'approval.frame.approval' : 'approval.frame.input')}
        description={approvalQuestion(approval, t)}
        variant={destructive ? 'destructive' : 'default'}
        dataAttributes={{ 'data-approval-token': approval.token }}
        {...(footer !== undefined ? { footer } : {})}
      >
        {children}
      </QuestionCard>
    );
  }

  if (isTerminal(approval)) {
    return frame(
      <QuestionReceipt tone="warning" label={t('approval.terminal.expired')} announce />,
    );
  }
  if (state !== 'pending') {
    const receipt = RECEIPTS[state];
    const summary =
      state === 'answered' && given !== ''
        ? [{ label: t('approval.card.freeText'), value: given }]
        : [];
    return frame(<QuestionReceipt tone={receipt.tone} label={t(receipt.key)} summary={summary} />);
  }

  const footer = (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={busy}
          onClick={() => {
            submit('decline');
          }}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('approval.card.decline')}
        </Button>
        <CancelControl
          isStreaming={isStreaming}
          disabled={busy}
          labels={{
            cancel: t('approval.card.cancel'),
            confirm: t('approval.card.confirmCancel'),
            yes: t('approval.card.confirmCancelYes'),
            no: t('approval.card.confirmCancelNo'),
          }}
          onCancel={() => {
            submit('cancel');
          }}
        />
      </div>
      <Button
        type="button"
        variant={destructive ? 'destructive' : 'default'}
        disabled={busy || (choosing && chosenOption === undefined)}
        onClick={answer}
        className="rounded-full text-[0.8125rem]"
      >
        {t(verb)}
      </Button>
    </>
  );

  return frame(
    <>
      {isApproval ? <p className="text-xs text-text-muted">{t('approval.frame.review')}</p> : null}
      {choosing ? (
        <QuestionOptions
          labelledBy={`${baseId}-title`}
          options={options.map((option, index) => ({
            id: String(index),
            label: scopeLabel(option),
          }))}
          mode="single"
          selected={new Set(chosen === null ? [] : [String(chosen)])}
          disabled={busy}
          onToggle={(id) => {
            setChosen(Number(id));
          }}
          onSubmit={answer}
        />
      ) : (
        <div className="flex flex-col gap-1">
          <Label
            htmlFor={`${baseId}-answer`}
            className="text-[0.75rem] font-normal text-text-muted"
          >
            {t('approval.card.freeText')}
          </Label>
          <Textarea
            id={`${baseId}-answer`}
            value={freeText}
            disabled={busy}
            onChange={(event) => {
              setFreeText(event.target.value);
            }}
            placeholder={t('approval.card.freeTextPlaceholder')}
            rows={2}
            aria-invalid={ariaInvalid(failed && freeText.trim().length === 0)}
            className="resize-y bg-surface text-sm"
          />
        </div>
      )}
      {failed ? (
        <Alert
          role="status"
          aria-live="polite"
          data-tone="danger"
          variant="destructive"
          className="bg-surface"
        >
          <AlertDescription>{t('approval.card.error')}</AlertDescription>
        </Alert>
      ) : null}
    </>,
    footer,
  );
}

interface FrameIconProps {
  readonly approval: boolean;
  readonly destructive: boolean;
}

function FrameIcon({ approval, destructive }: FrameIconProps) {
  if (destructive) return <TriangleAlert aria-hidden="true" className="size-5 text-danger" />;
  if (approval) return <ShieldCheck aria-hidden="true" className="size-5 text-warning" />;
  return <MessageSquareText aria-hidden="true" className="size-5 text-accent-text" />;
}

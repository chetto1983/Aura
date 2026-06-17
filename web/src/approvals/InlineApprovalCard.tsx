import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { isTerminal, parseOptions } from './approvalState';
import { useResolveApproval, type Approval, type ResolveAction } from './useApprovals';

// InlineApprovalCard (APRV-02/03 / D-03/D-05/D-06) is the "perfectly like Claude
// Code" in-thread card: an ask_user interrupt rendered IN that conversation's
// message stream. It shows the backend question VERBATIM (no client rewrite) +
// option buttons + a free-text input where the pause allows, and the three verbs:
//
//   Answer     → resolve {action:'accept', content}  → run resumes (D-05)
//   Decline    → resolve {action:'decline'}          → agent continues INFORMED of
//                the refusal; the operator's typed text is NOT sent as the answer
//                (deny != accept — the Pitfall-4 / T-25-17 footgun guard)
//   Cancel run → resolve {action:'cancel'}           → abort + auto-resolve; an
//                inline "Stop this run?" confirm shows only while streaming (NOT a
//                modal — keeps the Claude-Code-fast feel)
//
// After a successful accept/decline the parent re-drives the run (a no-Resume
// POST /agent/run; continue-after-resume) via onResolved so the stream continues
// over the rehydrated history.
//
// D-06 (T-25-18): an expired / auto-terminated interrupt renders its terminal state
// inline (warning) with the verbs disabled — never a silent disappearance (APRV-03).
//
// SECURITY (T-25-20): the question + option labels render as React text nodes
// (auto-escaped), never raw HTML.

type CardState = 'pending' | 'answered' | 'declined' | 'cancelled';

export interface InlineApprovalCardProps {
  readonly approval: Approval;
  /** True while the owning run is actively streaming — gates the Cancel confirm. */
  readonly isStreaming?: boolean;
  /** Re-drive the run after a successful accept/decline (continue-after-resume). */
  readonly onResolved?: (conversationId: string) => void;
}

export function InlineApprovalCard({ approval, isStreaming, onResolved }: InlineApprovalCardProps) {
  const { t } = useTranslation();
  const resolve = useResolveApproval();
  const options = parseOptions(approval.options);
  const terminal = isTerminal(approval);

  const [freeText, setFreeText] = useState('');
  const [confirmingCancel, setConfirmingCancel] = useState(false);
  // The local terminal chip after a successful verb (the badge poll also drops the
  // row, but the in-thread card transitions immediately for instant feedback).
  const [state, setState] = useState<CardState>('pending');

  const freeTextId = useId();
  const busy = resolve.isPending;
  const failed = resolve.isError;

  function submit(action: ResolveAction, content?: string) {
    resolve.mutate(
      // accept carries the operator content; decline/cancel never do (the hook
      // strips it, but be explicit at the call site too — deny != accept).
      action === 'accept'
        ? { token: approval.token, action, content: content ?? '' }
        : { token: approval.token, action },
      {
        onSuccess: () => {
          setState(
            action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled',
          );
          // accept/decline keep the run alive → re-drive it. cancel aborts it.
          if (action !== 'cancel') onResolved?.(approval.conversation_id);
        },
      },
    );
  }

  // D-06: terminal interrupt (expired / auto-resolved) — explicit state, verbs gone.
  if (terminal) {
    return (
      <CardShell question={approval.question}>
        <TerminalChip tone="warning" label={t('approval.terminal.expired')} />
      </CardShell>
    );
  }

  // Post-resolve terminal chips (the in-thread transition).
  if (state === 'answered') {
    return (
      <CardShell question={approval.question}>
        <TerminalChip tone="success" label={t('approval.card.answered')} />
      </CardShell>
    );
  }
  if (state === 'declined') {
    return (
      <CardShell question={approval.question}>
        <TerminalChip tone="success" label={t('approval.card.declined')} />
      </CardShell>
    );
  }
  if (state === 'cancelled') {
    return (
      <CardShell question={approval.question}>
        <TerminalChip tone="danger" label={t('approval.card.cancelled')} />
      </CardShell>
    );
  }

  return (
    <CardShell question={approval.question}>
      {options.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {options.map((option) => (
            <button
              key={option}
              type="button"
              disabled={busy}
              onClick={() => {
                submit('accept', option);
              }}
              className="min-h-9 rounded-[var(--radius-md)] border border-border bg-surface px-3 text-[0.8125rem] font-medium text-text outline-none hover:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60"
            >
              {option}
            </button>
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <label htmlFor={freeTextId} className="text-[0.6875rem] text-text-muted">
            {t('approval.card.freeText')}
          </label>
          <textarea
            id={freeTextId}
            value={freeText}
            onChange={(event) => {
              setFreeText(event.target.value);
            }}
            placeholder={t('approval.card.freeTextPlaceholder')}
            rows={2}
            // Omit aria-invalid when valid so React drops the attribute (axe).
            aria-invalid={(failed && freeText.trim().length === 0) || undefined}
            className="w-full resize-y rounded-[var(--radius-md)] border border-border bg-surface px-2 py-1.5 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {options.length === 0 ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              submit('accept', freeText);
            }}
            className="min-h-9 rounded-[var(--radius-md)] bg-accent px-3 text-[0.8125rem] font-medium text-[#0B0E14] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60"
          >
            {t('approval.card.answer')}
          </button>
        ) : null}
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            // Decline sends NO operator content — the agent continues informed of
            // the refusal, not treating the typed text as the answer (T-25-17).
            submit('decline');
          }}
          className="min-h-9 rounded-[var(--radius-md)] border border-border px-3 text-[0.8125rem] font-medium text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60"
        >
          {t('approval.card.decline')}
        </button>

        {confirmingCancel ? (
          <span className="flex items-center gap-2">
            <span className="text-[0.8125rem] text-warning">
              {t('approval.card.confirmCancel')}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                submit('cancel');
              }}
              className="min-h-9 rounded-[var(--radius-md)] px-3 text-[0.8125rem] font-medium text-danger outline-none hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60"
            >
              {t('approval.card.confirmCancelYes')}
            </button>
            <button
              type="button"
              onClick={() => {
                setConfirmingCancel(false);
              }}
              className="min-h-9 rounded-[var(--radius-md)] px-3 text-[0.8125rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {t('approval.card.confirmCancelNo')}
            </button>
          </span>
        ) : (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              // While streaming, ask for an inline confirm (NOT a modal). When the
              // run is idle, cancel immediately.
              if (isStreaming) setConfirmingCancel(true);
              else submit('cancel');
            }}
            className="min-h-9 rounded-[var(--radius-md)] px-3 text-[0.8125rem] font-medium text-danger outline-none hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60"
          >
            {t('approval.card.cancel')}
          </button>
        )}
      </div>

      {failed ? (
        <p role="alert" className="text-[0.8125rem] text-danger">
          {t('approval.card.error')}
        </p>
      ) : null}
    </CardShell>
  );
}

function CardShell({
  question,
  children,
}: {
  readonly question: string;
  readonly children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2 rounded-[var(--radius-md)] border border-accent/40 bg-surface-2 px-4 py-3">
      {/* The backend ask_user question, VERBATIM (no client-side rewrite). */}
      <p className="text-sm leading-relaxed text-text">{question}</p>
      {children}
    </div>
  );
}

type ChipTone = 'success' | 'warning' | 'danger';

const CHIP_TEXT: Record<ChipTone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
};
const CHIP_DOT: Record<ChipTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

function TerminalChip({ tone, label }: { readonly tone: ChipTone; readonly label: string }) {
  return (
    <span className={`flex items-center gap-1.5 text-[0.8125rem] ${CHIP_TEXT[tone]}`}>
      <span
        aria-hidden="true"
        className={`inline-block h-2 w-2 shrink-0 rounded-sm ${CHIP_DOT[tone]}`}
      />
      {label}
    </span>
  );
}

import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { isTerminal, parseOptions } from './approvalState';
import { useResolveApproval, type Approval, type ResolveAction } from './useApprovals';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

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
//
// Phase 29 (SKW-02 / D-11): a RISKY skill-install approval (Kind=approval, minted by the
// operator-origin governance-write pause) renders the SAME card with an added RISKY
// supply-chain framing strip ABOVE the existing verbs — a RISKY badge, the container-isolation
// note, and the resume token (mono). It carries NO run/activate affordance (activation is the
// approval RESUME only — prohibition #3/#4) and mints NO second badge/queue (the existing
// ApprovalBadge already counts it — D-11). The source/hash/preview travel inside the verbatim
// (React-escaped) question, never via dangerouslySetInnerHTML (T-25-20).

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
  // A RISKY skill-install approval (Kind=approval, operator-origin governance-write pause).
  // The RISKY framing strip renders above the verbs; it carries no run/activate affordance.
  const skillRisk = approval.kind === 'approval';
  const riskStrip = skillRisk ? <SkillRiskStrip token={approval.token} /> : undefined;

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
      <CardShell question={approval.question} header={riskStrip}>
        <TerminalChip tone="warning" label={t('approval.terminal.expired')} />
      </CardShell>
    );
  }

  // Post-resolve terminal chips (the in-thread transition).
  if (state === 'answered') {
    return (
      <CardShell question={approval.question} header={riskStrip}>
        <TerminalChip tone="success" label={t('approval.card.answered')} />
      </CardShell>
    );
  }
  if (state === 'declined') {
    return (
      <CardShell question={approval.question} header={riskStrip}>
        <TerminalChip tone="success" label={t('approval.card.declined')} />
      </CardShell>
    );
  }
  if (state === 'cancelled') {
    return (
      <CardShell question={approval.question} header={riskStrip}>
        <TerminalChip tone="danger" label={t('approval.card.cancelled')} />
      </CardShell>
    );
  }

  return (
    <CardShell question={approval.question} header={riskStrip}>
      {options.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {options.map((option) => (
            <Button
              key={option}
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => {
                submit('accept', option);
              }}
              className="text-[0.8125rem] hover:border-accent"
            >
              {option}
            </Button>
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <Label htmlFor={freeTextId} className="text-[0.75rem] font-normal text-text-muted">
            {t('approval.card.freeText')}
          </Label>
          <Textarea
            id={freeTextId}
            value={freeText}
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

      <div className="flex flex-wrap items-center gap-2">
        {options.length === 0 ? (
          <Button
            type="button"
            disabled={busy}
            onClick={() => {
              submit('accept', freeText);
            }}
            className="text-[0.8125rem]"
          >
            {t('approval.card.answer')}
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          disabled={busy}
          onClick={() => {
            // Decline sends NO operator content — the agent continues informed of
            // the refusal, not treating the typed text as the answer (T-25-17).
            submit('decline');
          }}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('approval.card.decline')}
        </Button>

        {confirmingCancel ? (
          <span className="flex items-center gap-2">
            <span className="text-[0.8125rem] text-warning">
              {t('approval.card.confirmCancel')}
            </span>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={() => {
                submit('cancel');
              }}
              className="text-[0.8125rem]"
            >
              {t('approval.card.confirmCancelYes')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setConfirmingCancel(false);
              }}
              className="text-[0.8125rem] text-text-muted hover:text-text"
            >
              {t('approval.card.confirmCancelNo')}
            </Button>
          </span>
        ) : (
          <Button
            type="button"
            variant="ghost"
            disabled={busy}
            onClick={() => {
              // While streaming, ask for an inline confirm (NOT a modal). When the
              // run is idle, cancel immediately.
              if (isStreaming) setConfirmingCancel(true);
              else submit('cancel');
            }}
            className="text-[0.8125rem] text-danger hover:bg-danger/15 hover:text-danger"
          >
            {t('approval.card.cancel')}
          </Button>
        )}
      </div>

      {failed ? (
        <Alert variant="destructive" className="bg-surface">
          <AlertDescription>{t('approval.card.error')}</AlertDescription>
        </Alert>
      ) : null}
    </CardShell>
  );
}

function CardShell({
  question,
  header,
  children,
}: {
  readonly question: string;
  /** Optional framing strip (the Phase-29 RISKY skill-install supply-chain note). */
  readonly header?: React.ReactNode;
  readonly children: React.ReactNode;
}) {
  return (
    <Card className="gap-2 border-accent/40 bg-surface-2 px-4 py-3">
      {header}
      {/* The backend ask_user question, VERBATIM (no client-side rewrite). */}
      <p className="text-sm leading-relaxed text-text">{question}</p>
      {children}
    </Card>
  );
}

/** SkillRiskStrip — the Phase-29 RISKY skill-install supply-chain framing (RISKY badge + the
 * container-isolation note + the resume token, mono). Rendered ABOVE the verbs; no run/activate
 * affordance (activation is the approval resume only — prohibition #3/#4). */
function SkillRiskStrip({ token }: { readonly token: string }) {
  const { t } = useTranslation();
  return (
    <Card className="gap-1 border-warning bg-warning/15 px-3 py-2">
      <Badge variant="warning" className="text-[0.8125rem]">
        <span aria-hidden="true" className="inline-block h-2 w-2 shrink-0 rounded-sm bg-warning" />
        {t('approval.skill.riskBadge')}
      </Badge>
      <span className="text-[0.8125rem] leading-relaxed text-text">
        {t('approval.skill.containerNote')}
      </span>
      <span className="flex flex-wrap items-center gap-1 text-[0.75rem] text-text-muted">
        {t('approval.skill.resumeToken')}
        <code className="break-all font-mono text-text">{token}</code>
      </span>
    </Card>
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
    <Badge
      variant={tone === 'danger' ? 'danger' : tone}
      className={`text-[0.8125rem] ${CHIP_TEXT[tone]}`}
    >
      <span
        aria-hidden="true"
        className={`inline-block h-2 w-2 shrink-0 rounded-sm ${CHIP_DOT[tone]}`}
      />
      {label}
    </Badge>
  );
}

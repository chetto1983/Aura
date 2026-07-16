import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { isTerminal, parseOptions } from './approvalState';
import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';
import { useResolveApproval, type Approval, type ResolveAction } from './useApprovals';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

// InlineApprovalCard renders a pending ask_user interrupt in its conversation.
// The complete server-sanitized question remains escaped React text. The DTO has
// no trusted install subtype, so framing stays generic and the URL capability
// token never appears as a dedicated visible field.

type CardState = 'pending' | 'answered' | 'declined' | 'cancelled';

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
  const terminal = isTerminal(approval);
  const [freeText, setFreeText] = useState('');
  const [confirmingCancel, setConfirmingCancel] = useState(false);
  const [state, setState] = useState<CardState>('pending');
  const cancelButtonRef = useRef<HTMLButtonElement | null>(null);
  const keepRunningButtonRef = useRef<HTMLButtonElement | null>(null);
  const attemptSequence = useRef(0);
  const freeTextId = useId();
  const busy = resolve.isPending;
  const failed = resolve.isError;
  const frame = <ApprovalFrame kind={approval.kind} />;

  useEffect(() => {
    if (confirmingCancel) keepRunningButtonRef.current?.focus();
  }, [confirmingCancel]);

  function closeCancelConfirmation() {
    setConfirmingCancel(false);
    requestAnimationFrame(() => cancelButtonRef.current?.focus());
  }

  function handleCancelConfirmationKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    closeCancelConfirmation();
  }

  function submit(action: ResolveAction, content?: string) {
    attemptSequence.current += 1;
    const attempt: ApprovalResolution = {
      approval,
      action,
      attemptId: `${freeTextId}:${String(attemptSequence.current)}`,
    };
    onResolutionStarted?.(attempt);
    resolve.mutate(
      action === 'accept'
        ? { token: approval.token, action, content: content ?? '' }
        : { token: approval.token, action },
      {
        onSuccess: () => {
          setState(
            action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled',
          );
          void onResolved?.(attempt);
        },
        onError: () => {
          void onResolutionFailed?.(attempt);
        },
      },
    );
  }

  if (terminal) {
    return (
      <CardShell approval={approval} header={frame}>
        <TerminalChip tone="warning" label={t('approval.terminal.expired')} announce />
      </CardShell>
    );
  }

  if (state === 'answered') {
    return (
      <CardShell approval={approval} header={frame}>
        <TerminalChip tone="success" label={t('approval.card.answered')} />
      </CardShell>
    );
  }
  if (state === 'declined') {
    return (
      <CardShell approval={approval} header={frame}>
        <TerminalChip tone="neutral" label={t('approval.card.declined')} />
      </CardShell>
    );
  }
  if (state === 'cancelled') {
    return (
      <CardShell approval={approval} header={frame}>
        <TerminalChip tone="danger" label={t('approval.card.cancelled')} />
      </CardShell>
    );
  }

  return (
    <CardShell approval={approval} header={frame}>
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
              onKeyDown={handleCancelConfirmationKeyDown}
              onClick={() => {
                submit('cancel');
              }}
              className="min-h-11 text-[0.8125rem]"
            >
              {t('approval.card.confirmCancelYes')}
            </Button>
            <Button
              ref={keepRunningButtonRef}
              type="button"
              variant="ghost"
              size="sm"
              onKeyDown={handleCancelConfirmationKeyDown}
              onClick={closeCancelConfirmation}
              className="min-h-11 text-[0.8125rem] text-text-muted hover:text-text"
            >
              {t('approval.card.confirmCancelNo')}
            </Button>
          </span>
        ) : (
          <Button
            ref={cancelButtonRef}
            type="button"
            variant="ghost"
            disabled={busy}
            onClick={() => {
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
    </CardShell>
  );
}

function CardShell({
  approval,
  header,
  children,
}: {
  readonly approval: Approval;
  readonly header: ReactNode;
  readonly children: ReactNode;
}) {
  return (
    <Card
      data-approval-token={approval.token}
      tabIndex={-1}
      className="gap-2 border-accent/40 bg-surface-2 px-4 py-3"
    >
      {header}
      <p className="overflow-x-auto text-sm leading-relaxed whitespace-pre-wrap text-text break-words [overflow-wrap:anywhere]">
        {approval.question}
      </p>
      {children}
    </Card>
  );
}

function ApprovalFrame({ kind }: { readonly kind: string }) {
  const { t } = useTranslation();
  const isApproval = kind === 'approval';
  return (
    <div className="flex flex-col gap-1 border-s-2 border-warning ps-3">
      <span className="text-sm font-medium text-text">
        {t(isApproval ? 'approval.frame.approval' : 'approval.frame.input')}
      </span>
      {isApproval ? (
        <span className="text-xs text-text-muted">{t('approval.frame.review')}</span>
      ) : null}
    </div>
  );
}

type ChipTone = 'success' | 'neutral' | 'warning' | 'danger';

const CHIP_TEXT: Record<ChipTone, string> = {
  success: 'text-success',
  neutral: 'text-text-muted',
  warning: 'text-warning',
  danger: 'text-danger',
};
const CHIP_DOT: Record<ChipTone, string> = {
  success: 'bg-success',
  neutral: 'bg-text-muted',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

function TerminalChip({
  tone,
  label,
  announce = false,
}: {
  readonly tone: ChipTone;
  readonly label: string;
  readonly announce?: boolean;
}) {
  return (
    <Badge
      variant={tone === 'neutral' ? 'secondary' : tone}
      data-tone={tone}
      {...(announce ? { role: 'status', 'aria-live': 'polite' as const } : {})}
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

import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { isTerminal } from './approvalState';
import { useApprovals, type Approval } from './useApprovals';

// ApprovalList (APRV-01 / D-04) is the lightweight cross-thread popover the header
// badge opens: one row per pending ask_user across ALL threads. Each row shows the
// conversation title + the (sanitized) question and an `Open` action that navigates
// to /c/:conversationId — where the inline card (Task 2) lives — so the operator
// jumps straight to resolving it in place (resolution stays inline, D-03).
//
// D-06 (T-25-18): an expired / auto-terminated row renders its explicit terminal
// state (`Expired — auto-resolved.`, text-warning) — it is NEVER silently dropped,
// so the operator does not lose track of a background HITL.
//
// Questions render as React text nodes (auto-escaped); the row never uses raw HTML
// (T-25-20).

export interface ApprovalListProps {
  /** Title lookup for a conversation id (the sidebar's cached titles); falls back
      to a localised "Untitled" when unknown. */
  readonly titleFor?: (conversationId: string) => string | undefined;
  /** Select the opened thread so the AppShell binds it (alongside the navigate). */
  readonly onOpen?: (conversationId: string) => void;
}

export function ApprovalList({ titleFor, onOpen }: ApprovalListProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data } = useApprovals();
  const rows = data ?? [];

  function open(conversationId: string) {
    onOpen?.(conversationId);
    void navigate(`/c/${encodeURIComponent(conversationId)}`);
  }

  return (
    <div
      role="region"
      aria-label={t('approval.list.label')}
      className="flex max-h-80 w-80 flex-col gap-1 overflow-y-auto rounded-[var(--radius-md)] border border-border bg-surface p-2 shadow-lg"
    >
      {rows.length === 0 ? (
        <p className="px-2 py-3 text-sm text-text-muted">{t('approval.list.empty')}</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {rows.map((row) => (
            <ApprovalRow
              key={row.token}
              row={row}
              title={titleFor?.(row.conversation_id) ?? t('approval.list.untitled')}
              onOpen={() => {
                open(row.conversation_id);
              }}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

interface ApprovalRowProps {
  readonly row: Approval;
  readonly title: string;
  readonly onOpen: () => void;
}

function ApprovalRow({ row, title, onOpen }: ApprovalRowProps) {
  const { t } = useTranslation();
  const terminal = isTerminal(row);

  return (
    <li className="rounded-[var(--radius-md)] border border-border bg-surface-2 px-2 py-1.5">
      <p className="truncate text-[0.8125rem] font-medium text-text">
        {title} <span className="text-text-faint">·</span>{' '}
        <span className="font-normal text-text-muted">{row.question}</span>
      </p>
      <div className="mt-1 flex items-center justify-between gap-2">
        {terminal ? (
          // D-06: explicit terminal state, never silently dropped. Icon + text +
          // warning tone (never colour alone).
          <span className="flex items-center gap-1.5 text-[0.6875rem] text-warning">
            <span
              aria-hidden="true"
              className="inline-block h-2 w-2 shrink-0 rounded-sm bg-warning"
            />
            {t('approval.terminal.expired')}
          </span>
        ) : (
          <span className="text-[0.6875rem] text-text-faint">
            {t(`approval.kind.${row.kind}`, row.kind)}
          </span>
        )}
        <button
          type="button"
          onClick={onOpen}
          className="rounded-[var(--radius-sm)] px-2 py-0.5 text-[0.8125rem] font-medium text-accent outline-none hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('approval.list.open')}
        </button>
      </div>
    </li>
  );
}

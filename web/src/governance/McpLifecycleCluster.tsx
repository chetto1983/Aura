import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  removeMcpServer,
  setMcpServerEnabled,
  trustMcpServer,
  type McpServerRow,
} from './governanceApi';

// McpLifecycleCluster (MCPW-02/03) — the inline control cluster on the MCP server detail header
// (NOT a kebab): an enable/disable toggle (success/muted dot + label, idempotent), a
// `Trust & approve` inline form requiring a reason, and a `Remove server` destructive
// confirmation dialog with action-specific labels (Remove server / Keep server), the
// destructive button NOT default-focused, Escape-dismissable + focus-trapped. Status is never
// color-alone — every state carries a dot + a text label (WCAG 1.4.1).

const TRUST_CLASS_LOCAL = 'trusted_local';

export interface McpLifecycleClusterProps {
  readonly server: McpServerRow;
  readonly onRemoved: () => void;
}

export function McpLifecycleCluster({ server, onRemoved }: McpLifecycleClusterProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [trusting, setTrusting] = useState(false);
  const [reason, setReason] = useState('');
  const [confirmingRemove, setConfirmingRemove] = useState(false);
  const reasonId = useId();

  // A blocked server reads as disabled; an enabled flag is absent on the read row, so the
  // toggle reflects whether the server is runnable (not blocked). The backend is the authority.
  const enabled = server.trust !== 'blocked';

  function invalidate() {
    void queryClient.invalidateQueries({ queryKey: ['governance', 'mcp'] });
  }

  const enableMutation = useMutation({
    mutationFn: (next: boolean) => setMcpServerEnabled(server.name, next),
    onSuccess: invalidate,
  });
  const trustMutation = useMutation({
    mutationFn: (vars: { readonly reason: string }) =>
      trustMcpServer(server.name, TRUST_CLASS_LOCAL, vars.reason),
    onSuccess: () => {
      invalidate();
      setTrusting(false);
      setReason('');
    },
  });
  const removeMutation = useMutation({
    mutationFn: () => removeMcpServer(server.name),
    onSuccess: () => {
      invalidate();
      setConfirmingRemove(false);
      onRemoved();
    },
  });

  return (
    <section
      aria-label={t('governance.mcp.lifecycle.toolsHeading')}
      className="flex flex-col gap-3"
    >
      <div className="flex flex-wrap items-center gap-2">
        {/* Enable/disable toggle — never color-alone (dot + label). */}
        <button
          type="button"
          aria-pressed={enabled}
          disabled={enableMutation.isPending}
          onClick={() => {
            enableMutation.mutate(!enabled);
          }}
          className="flex min-h-[44px] items-center gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${
              enabled ? 'bg-success' : 'bg-text-muted'
            }`}
          />
          <span className={enabled ? 'text-success' : 'text-text-muted'}>
            {enabled
              ? t('governance.mcp.lifecycle.enabled')
              : t('governance.mcp.lifecycle.disabled')}
          </span>
          <span className="text-text-muted">
            ·{' '}
            {enabled ? t('governance.mcp.lifecycle.disable') : t('governance.mcp.lifecycle.enable')}
          </span>
        </button>

        <button
          type="button"
          onClick={() => {
            setTrusting((prev) => !prev);
          }}
          className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t('governance.mcp.lifecycle.trust')}
        </button>

        <button
          type="button"
          onClick={() => {
            setConfirmingRemove(true);
          }}
          className="min-h-[44px] rounded-md border border-danger px-3 py-2 text-[13px] font-semibold text-danger outline-none hover:bg-danger/10 focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t('governance.mcp.lifecycle.remove')}
        </button>
      </div>

      {/* Trust & approve inline form — requires a reason. */}
      {trusting ? (
        <form
          className="flex flex-col gap-2 rounded-md border border-border bg-surface-2 px-3 py-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (reason.trim() === '') return;
            trustMutation.mutate({ reason: reason.trim() });
          }}
        >
          <label htmlFor={reasonId} className="text-[13px] font-semibold text-text">
            {t('governance.mcp.lifecycle.trustReasonLabel')}
          </label>
          <input
            id={reasonId}
            type="text"
            value={reason}
            onChange={(event) => {
              setReason(event.target.value);
            }}
            placeholder={t('governance.mcp.lifecycle.trustReasonPlaceholder')}
            className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
          <div className="flex flex-wrap gap-2">
            <button
              type="submit"
              disabled={reason.trim() === '' || trustMutation.isPending}
              className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              {t('governance.mcp.lifecycle.trustConfirm')}
            </button>
            <button
              type="button"
              onClick={() => {
                setTrusting(false);
                setReason('');
              }}
              className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t('governance.mcp.lifecycle.trustCancel')}
            </button>
          </div>
        </form>
      ) : null}

      {confirmingRemove ? (
        <RemoveDialog
          name={server.name}
          pending={removeMutation.isPending}
          onConfirm={() => {
            removeMutation.mutate();
          }}
          onCancel={() => {
            setConfirmingRemove(false);
          }}
        />
      ) : null}
    </section>
  );
}

/** A focus-trapped, Escape-dismissable destructive confirmation dialog. The destructive
 * button is NOT default-focused — focus lands on the safe `Keep server` action (NN/g). */
function RemoveDialog({
  name,
  pending,
  onConfirm,
  onCancel,
}: {
  readonly name: string;
  readonly pending: boolean;
  readonly onConfirm: () => void;
  readonly onCancel: () => void;
}) {
  const { t } = useTranslation();
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const titleId = useId();
  const bodyId = useId();

  useEffect(() => {
    // The safe (Keep server) action is default-focused, NOT the destructive Remove (NN/g).
    cancelRef.current?.focus();
    const dialog = dialogRef.current;
    if (dialog === null) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCancel();
        return;
      }
      if (event.key !== 'Tab' || dialog === null) return;
      const items = Array.from(dialog.querySelectorAll<HTMLElement>('button')).filter(
        (el) => !el.hasAttribute('disabled'),
      );
      if (items.length === 0) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (first === undefined || last === undefined) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [onCancel]);

  return (
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={bodyId}
      className="flex flex-col gap-3 rounded-md border border-danger bg-surface-2 px-4 py-3"
    >
      <h4 id={titleId} className="font-display text-[20px] font-semibold text-text">
        {t('governance.mcp.lifecycle.removeTitle', { name })}
      </h4>
      <p id={bodyId} className="text-[15.5px] leading-relaxed text-text">
        {t('governance.mcp.lifecycle.removeBody')}
      </p>
      <div className="flex flex-wrap gap-2">
        <button
          ref={cancelRef}
          type="button"
          onClick={onCancel}
          className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t('governance.mcp.lifecycle.removeCancel')}
        </button>
        <button
          type="button"
          disabled={pending}
          onClick={onConfirm}
          className="min-h-[44px] rounded-md border border-danger px-4 py-2 text-[13px] font-semibold text-danger outline-none hover:bg-danger/10 focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
          {t('governance.mcp.lifecycle.removeConfirm')}
        </button>
      </div>
    </div>
  );
}

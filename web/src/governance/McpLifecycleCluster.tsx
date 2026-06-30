import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Power, ShieldCheck, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { trapTabKey } from '../a11y/focusTrap';
import { Spinner } from '../components/Spinner';
import {
  removeMcpServer,
  setMcpServerEnabled,
  trustMcpServer,
  type McpServerRow,
} from './governanceApi';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

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
        <Button
          type="button"
          variant="outline"
          aria-pressed={enabled}
          disabled={enableMutation.isPending}
          onClick={() => {
            enableMutation.mutate(!enabled);
          }}
        >
          <Power data-icon aria-hidden="true" className="size-4" />
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
        </Button>

        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setTrusting((prev) => !prev);
          }}
        >
          <ShieldCheck data-icon aria-hidden="true" className="size-4" />
          {t('governance.mcp.lifecycle.trust')}
        </Button>

        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            setConfirmingRemove(true);
          }}
          className="border border-danger text-danger hover:bg-danger/10 hover:text-danger"
        >
          <Trash2 data-icon aria-hidden="true" className="size-4" />
          {t('governance.mcp.lifecycle.remove')}
        </Button>
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
          <Label htmlFor={reasonId} className="text-[13px] font-semibold text-text">
            {t('governance.mcp.lifecycle.trustReasonLabel')}
          </Label>
          <Input
            id={reasonId}
            type="text"
            value={reason}
            onChange={(event) => {
              setReason(event.target.value);
            }}
            placeholder={t('governance.mcp.lifecycle.trustReasonPlaceholder')}
            className="text-[13px]"
          />
          <div className="flex flex-wrap gap-2">
            <Button
              type="submit"
              disabled={reason.trim() === '' || trustMutation.isPending}
              aria-busy={trustMutation.isPending}
            >
              {trustMutation.isPending ? <Spinner /> : null}
              {t('governance.mcp.lifecycle.trustConfirm')}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setTrusting(false);
                setReason('');
              }}
            >
              {t('governance.mcp.lifecycle.trustCancel')}
            </Button>
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
      trapTabKey(event, dialog);
    }
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [onCancel]);

  return (
    <Card
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={bodyId}
      className="gap-3 border-danger bg-surface-2 px-4 py-3"
    >
      <h4 id={titleId} className="font-display text-[20px] font-semibold text-text">
        {t('governance.mcp.lifecycle.removeTitle', { name })}
      </h4>
      <p id={bodyId} className="text-[15.5px] leading-relaxed text-text">
        {t('governance.mcp.lifecycle.removeBody')}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button ref={cancelRef} type="button" variant="outline" onClick={onCancel}>
          {t('governance.mcp.lifecycle.removeCancel')}
        </Button>
        <Button
          type="button"
          variant="ghost"
          disabled={pending}
          onClick={onConfirm}
          className="border border-danger text-danger hover:bg-danger/10 hover:text-danger"
        >
          {t('governance.mcp.lifecycle.removeConfirm')}
        </Button>
      </div>
    </Card>
  );
}

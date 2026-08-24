import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Power, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { removeMcpServer, setMcpServerEnabled, type McpServerRow } from './governanceApi';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// McpLifecycleCluster (MCPW-02/03) — the inline control cluster on the MCP server detail
// header (NOT a kebab): an enable/disable toggle (success/muted dot + label, idempotent) and
// a `Remove server` destructive confirmation dialog with action-specific labels (Remove
// server / Keep server), the destructive button NOT default-focused, Escape-dismissable +
// focus-trapped. Status is never color-alone — every state carries a dot + a text label
// (WCAG 1.4.1).
//
// There was a third control, `Trust & approve`. It is gone, along with the blocked-by-
// default class it existed to lift: installing a server IS the authorization, so the button
// changed nothing on every row an operator would ever click it on — while sitting next to
// Remove, which changes a great deal.

export interface McpLifecycleClusterProps {
  readonly server: McpServerRow;
  readonly onRemoved: () => void;
}

export function McpLifecycleCluster({ server, onRemoved }: McpLifecycleClusterProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [confirmingRemove, setConfirmingRemove] = useState(false);

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

      {confirmingRemove ? (
        <RemoveDialog
          open={confirmingRemove}
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
  open,
  name,
  pending,
  onConfirm,
  onCancel,
}: {
  readonly open: boolean;
  readonly name: string;
  readonly pending: boolean;
  readonly onConfirm: () => void;
  readonly onCancel: () => void;
}) {
  const { t } = useTranslation();

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onCancel();
        }
      }}
      role="alertdialog"
      title={t('governance.mcp.lifecycle.removeTitle', { name })}
      description={t('governance.mcp.lifecycle.removeBody')}
      cancelLabel={t('governance.mcp.lifecycle.removeCancel')}
      confirmLabel={t('governance.mcp.lifecycle.removeConfirm')}
      confirmPending={pending}
      onConfirm={onConfirm}
    />
  );
}

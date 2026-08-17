import { AttachmentPrimitive } from '@assistant-ui/react';
import type { Attachment, AttachmentStatus } from '@assistant-ui/core';
import { X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

// AttachmentChip: one composer attachment, rendered inside ComposerPrimitive.Attachments.
//
// The attachment arrives from ComposerPrimitive.Attachments' render function — the same
// object the send gate reads — instead of an upload item kept in component state beside it.
// Removal goes through AttachmentPrimitive.Remove, which calls the adapter's remove(), so
// the DELETE and the chip disappearing are one action rather than two that can disagree.

export function AttachmentChip({
  attachment,
  disabled = false,
}: {
  readonly attachment: Attachment;
  readonly disabled?: boolean;
}) {
  const { t } = useTranslation();
  const { name, status } = attachment;

  return (
    <AttachmentPrimitive.Root className="inline-flex min-h-10 min-w-0 max-w-full items-center gap-2 rounded-md border border-border bg-surface-2 px-2 py-1 text-xs text-text">
      <span className="min-w-0 truncate">{name}</span>
      <Badge variant={statusVariant(status)} className="shrink-0 text-[0.75rem]">
        {statusLabel(status, t)}
      </Badge>
      <AttachmentPrimitive.Remove
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={t('chat.attachments.remove', { name })}
            data-required-touch-target
            disabled={disabled}
            className="h-[44px] min-h-[44px] w-[44px] min-w-[44px] shrink-0 rounded-full text-text-muted hover:bg-surface-3 hover:text-text"
          >
            <X data-icon aria-hidden="true" className="size-3.5" />
          </Button>
        }
      />
    </AttachmentPrimitive.Root>
  );
}

function statusVariant(status: AttachmentStatus): 'secondary' | 'warning' | 'success' | 'danger' {
  switch (status.type) {
    case 'complete':
    case 'requires-action':
      return 'success';
    case 'running':
      return 'warning';
    case 'incomplete':
      return 'danger';
  }
}

// The four upload words the operator used to see map onto the library's three states. The
// one that needs care is `running`: the adapter holds it while the bytes move AND while the
// document pipeline indexes, so a full bar still said "Uploading 100%" for as long as
// indexing took (seen live 2026-08-17). Progress at 1 means the upload is done and the
// server is working, which is what "processing" always meant here.
function statusLabel(status: AttachmentStatus, t: ReturnType<typeof useTranslation>['t']): string {
  switch (status.type) {
    case 'running':
      return status.progress >= 1
        ? t('chat.attachments.processing')
        : t('chat.attachments.uploading', { progress: Math.round(status.progress * 100) });
    case 'incomplete':
      return t('chat.attachments.failed');
    case 'requires-action':
    case 'complete':
      return t('chat.attachments.ready');
  }
}

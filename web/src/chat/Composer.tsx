import { useAuiState, ComposerPrimitive } from '@assistant-ui/react';
import { useTranslation } from 'react-i18next';

// Composer: the query input + the Send↔Stop swap. Enter sends / Shift+Enter
// newlines / Esc cancels are handled by ComposerPrimitive.Input. Stop is
// ComposerPrimitive.Cancel → api.thread().cancelRun() → the external-store
// onCancel aborts the in-flight fetch (the server streamSSE unwinds on ctx.Done).
//
// Accent is reserved for the primary Send CTA only (UI-SPEC §Color list item 1);
// Stop is a neutral danger-tinted control so the accent stays scarce.

export function Composer() {
  const { t } = useTranslation();
  const isRunning = useAuiState((s) => s.thread.isRunning);

  return (
    <ComposerPrimitive.Root className="flex items-end gap-2 border-t border-border bg-bg px-3 py-2 sm:px-4">
      <ComposerPrimitive.Input
        rows={1}
        placeholder={t('chat.composer.placeholder')}
        aria-label={t('chat.composer.placeholder')}
        className="max-h-40 min-h-[var(--row-h)] flex-1 resize-none rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 py-2 text-sm leading-relaxed text-text outline-none placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      />
      {isRunning ? (
        <ComposerPrimitive.Cancel
          aria-label={t('chat.composer.stopAria')}
          className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] border border-danger px-4 text-sm font-medium text-danger outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.composer.stop')}
        </ComposerPrimitive.Cancel>
      ) : (
        <ComposerPrimitive.Send
          aria-label={t('chat.composer.sendAria')}
          className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] bg-accent px-4 text-sm font-medium text-[#0B0E14] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-70"
        >
          {t('chat.composer.send')}
        </ComposerPrimitive.Send>
      )}
    </ComposerPrimitive.Root>
  );
}

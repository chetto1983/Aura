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
    <ComposerPrimitive.Root className="mx-3 mb-3 flex items-end gap-2 rounded-[var(--radius-xl)] border border-border bg-surface p-2 shadow-[var(--shadow-popover)] sm:mx-4">
      <ComposerPrimitive.Input
        rows={1}
        placeholder={t('chat.composer.placeholder')}
        aria-label={t('chat.composer.placeholder')}
        className="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-3 py-2 text-[1.0625rem] leading-relaxed text-text outline-none placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      />
      {isRunning ? (
        <ComposerPrimitive.Cancel
          aria-label={t('chat.composer.stopAria')}
          className="flex min-h-10 min-w-10 items-center justify-center rounded-full bg-accent text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          <span aria-hidden="true" className="h-3 w-3 rounded-[3px] bg-current" />
        </ComposerPrimitive.Cancel>
      ) : (
        <ComposerPrimitive.Send
          aria-label={t('chat.composer.sendAria')}
          className="flex min-h-10 min-w-10 items-center justify-center rounded-full bg-accent text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:bg-surface-3 disabled:text-text-disabled"
        >
          <ArrowUpIcon />
        </ComposerPrimitive.Send>
      )}
    </ComposerPrimitive.Root>
  );
}

function ArrowUpIcon() {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 19V5" />
      <path d="m5 12 7-7 7 7" />
    </svg>
  );
}

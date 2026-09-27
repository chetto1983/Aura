import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Button } from '@/components/ui/button';

// CancelControl is a question's Cancel, confirmed inline while its run streams. Escape
// dismisses the confirmation and never cancels anything.

export interface CancelLabels {
  readonly cancel: string;
  readonly confirm: string;
  readonly yes: string;
  readonly no: string;
}

export interface CancelControlProps {
  readonly isStreaming?: boolean | undefined;
  readonly disabled: boolean;
  readonly labels: CancelLabels;
  readonly onCancel: () => void;
}

export function CancelControl({ isStreaming, disabled, labels, onCancel }: CancelControlProps) {
  const [confirming, setConfirming] = useState(false);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const keepRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirming) keepRef.current?.focus();
  }, [confirming]);

  function close() {
    setConfirming(false);
    requestAnimationFrame(() => cancelRef.current?.focus());
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    close();
  }

  if (confirming) {
    return (
      <span className="flex items-center gap-2">
        <span className="text-[0.8125rem] text-warning">{labels.confirm}</span>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          disabled={disabled}
          onKeyDown={onKeyDown}
          onClick={onCancel}
          className="min-h-11 text-[0.8125rem]"
        >
          {labels.yes}
        </Button>
        <Button
          ref={keepRef}
          type="button"
          variant="ghost"
          size="sm"
          onKeyDown={onKeyDown}
          onClick={close}
          className="min-h-11 text-[0.8125rem] text-text-muted hover:text-text"
        >
          {labels.no}
        </Button>
      </span>
    );
  }
  return (
    <Button
      ref={cancelRef}
      type="button"
      variant="ghost"
      disabled={disabled}
      onClick={() => {
        if (isStreaming === true) setConfirming(true);
        else onCancel();
      }}
      className="text-[0.8125rem] text-danger hover:bg-danger/15 hover:text-danger"
    >
      {labels.cancel}
    </Button>
  );
}

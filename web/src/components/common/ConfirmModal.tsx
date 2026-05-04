import { useEffect, useRef, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import {
  _subscribe,
  resolveCancelled,
  type ActiveRequest,
  type PromptOptions,
} from '@/lib/confirmModal';
import { useLocale } from '@/hooks/useLocale';

// Mount once at the app root. Imperative callers use confirm() / prompt()
// from @/lib/confirmModal; this component renders whatever request is
// currently active inside the existing Radix Dialog primitive.

export function ConfirmHost() {
  const [active, setActive] = useState<ActiveRequest | null>(null);
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const activeRef = useRef<ActiveRequest | null>(null);

  // Sync the ref *after* render so callbacks can read the latest active
  // request without violating react-hooks/refs (no writes during render).
  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  useEffect(() => {
    const unsubscribe = _subscribe((req) => {
      // If a request lands while another is active (e.g. async race),
      // resolve the old one as cancelled so its caller doesn't hang.
      setActive((prev) => {
        if (prev) resolveCancelled(prev);
        return req;
      });
      setValue(req.kind === 'prompt' ? (req.opts.defaultValue ?? '') : '');
      setError(null);
      setOpen(true);
    });
    return () => {
      unsubscribe();
      const current = activeRef.current;
      if (current) resolveCancelled(current);
    };
  }, []);

  // Auto-focus the prompt input or the cancel button (for destructive
  // confirms) so Enter doesn't immediately destroy data.
  useEffect(() => {
    if (!open || !active) return;
    const id = window.setTimeout(() => {
      if (active.kind === 'prompt') {
        inputRef.current?.select();
      } else if (active.opts.destructive) {
        cancelRef.current?.focus();
      }
    }, 50);
    return () => window.clearTimeout(id);
  }, [open, active]);

  const closeWith = (result: boolean | string | null) => {
    const current = activeRef.current;
    if (!current) return;
    if (current.kind === 'confirm') {
      (current.resolve as (v: boolean) => void)(result === true);
    } else {
      (current.resolve as (v: string | null) => void)(
        typeof result === 'string' ? result : null,
      );
    }
    setOpen(false);
    // Clear the request after the close animation finishes so the
    // dialog content stays mounted while it animates out.
    window.setTimeout(() => setActive(null), 150);
  };

  const onConfirm = () => {
    if (!active) return;
    if (active.kind === 'confirm') {
      closeWith(true);
      return;
    }
    const promptOpts = active.opts as PromptOptions;
    const trimmed = value.trim();
    if (promptOpts.validate) {
      const msg = promptOpts.validate(trimmed);
      if (msg) {
        setError(msg);
        return;
      }
    }
    closeWith(trimmed);
  };

  const { t } = useLocale();
  const isPrompt = active?.kind === 'prompt';
  const opts = active?.opts;
  const confirmLabel = opts?.confirmLabel ?? (opts?.destructive ? t('common.delete') : t('common.confirm'));
  const cancelLabel = opts?.cancelLabel ?? t('common.cancel');

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) closeWith(isPrompt ? null : false);
      }}
    >
      <DialogContent
        className="sm:max-w-md"
        data-testid="confirm-modal"
        onKeyDown={(e) => {
          if (e.key === 'Enter' && isPrompt) {
            e.preventDefault();
            onConfirm();
          }
        }}
      >
        {active && opts && (
          <>
            <DialogHeader>
              <DialogTitle>{opts.title}</DialogTitle>
              {opts.description && (
                <DialogDescription>{opts.description}</DialogDescription>
              )}
            </DialogHeader>

            {isPrompt && (
              <div className="space-y-1.5">
                {(opts as PromptOptions).label && (
                  <label
                    className="text-xs text-muted-foreground"
                    htmlFor="confirm-modal-input"
                  >
                    {(opts as PromptOptions).label}
                  </label>
                )}
                <input
                  id="confirm-modal-input"
                  data-testid="confirm-modal-input"
                  ref={inputRef}
                  type="text"
                  autoFocus
                  inputMode={(opts as PromptOptions).inputMode}
                  placeholder={(opts as PromptOptions).placeholder}
                  value={value}
                  onChange={(e) => {
                    setValue(e.target.value);
                    if (error) setError(null);
                  }}
                  className="min-h-11 w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary/40"
                />
                {error && (
                  <p
                    data-testid="confirm-modal-error"
                    className="text-xs text-destructive"
                    role="alert"
                  >
                    {error}
                  </p>
                )}
              </div>
            )}

            <DialogFooter>
              <Button
                ref={cancelRef}
                variant="outline"
                size="lg"
                data-testid="confirm-modal-cancel"
                onClick={() => closeWith(isPrompt ? null : false)}
              >
                {cancelLabel}
              </Button>
              <Button
                variant={opts.destructive ? 'destructive' : 'default'}
                size="lg"
                data-testid="confirm-modal-confirm"
                onClick={onConfirm}
              >
                {confirmLabel}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

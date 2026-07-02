import { type ReactNode, useRef } from 'react';
import { Button, type ButtonProps } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

export interface ConfirmDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly children?: ReactNode;
  readonly cancelLabel: ReactNode;
  readonly confirmLabel: ReactNode;
  readonly onConfirm: () => void | Promise<void>;
  readonly confirmVariant?: ButtonProps['variant'];
  readonly confirmDisabled?: boolean;
  readonly confirmPending?: boolean;
  readonly confirmIcon?: ReactNode;
  readonly role?: 'dialog' | 'alertdialog';
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  cancelLabel,
  confirmLabel,
  onConfirm,
  confirmVariant = 'destructive',
  confirmDisabled = false,
  confirmPending = false,
  confirmIcon,
  role = 'dialog',
}: ConfirmDialogProps) {
  const cancelRef = useRef<HTMLButtonElement>(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        role={role}
        aria-modal="true"
        showCloseButton={false}
        className="max-h-[calc(100vh-2rem)] w-[calc(100vw-2rem)] max-w-sm overflow-y-auto sm:max-w-md"
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          cancelRef.current?.focus();
        }}
      >
        <DialogHeader>
          <DialogTitle className="text-base">{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        {children}
        <DialogFooter className="flex-col-reverse items-stretch sm:flex-row sm:items-center">
          <Button
            ref={cancelRef}
            type="button"
            variant="outline"
            onClick={() => {
              onOpenChange(false);
            }}
          >
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant={confirmVariant}
            disabled={confirmDisabled || confirmPending}
            aria-busy={confirmPending ? 'true' : undefined}
            onClick={() => {
              void onConfirm();
            }}
          >
            {confirmIcon}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

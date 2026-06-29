import type { ReactNode } from 'react';
import { X } from 'lucide-react';
import { Button } from '@/components/ui/button';

export function OnboardingDialog({
  children,
  closeLabel,
  describedBy,
  kicker,
  onClose,
  title,
  titleId,
}: {
  readonly children: ReactNode;
  readonly closeLabel: string;
  readonly describedBy?: string;
  readonly kicker?: ReactNode;
  readonly onClose: () => void;
  readonly title: ReactNode;
  readonly titleId: string;
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={describedBy}
      className="fixed inset-0 z-50 flex flex-col bg-bg text-text"
    >
      <header className="flex shrink-0 items-center justify-between gap-3 border-b border-border bg-surface px-4 py-3">
        {kicker === undefined ? (
          <h1 id={titleId} className="font-display text-[18px] font-semibold text-text">
            {title}
          </h1>
        ) : (
          <div className="min-w-0">
            <p className="text-xs font-semibold text-accent-text">{kicker}</p>
            <h1 id={titleId} className="font-display text-[18px] font-semibold text-text">
              {title}
            </h1>
          </div>
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={closeLabel}
          onClick={onClose}
          className="text-text-muted hover:text-text"
        >
          <X data-icon aria-hidden="true" className="size-4" />
        </Button>
      </header>
      {children}
    </div>
  );
}

export function OnboardingCenteredState({
  message,
  muted = false,
  onRetry,
  retryLabel,
  role = 'status',
}: {
  readonly message: string;
  readonly muted?: boolean;
  readonly onRetry?: () => void;
  readonly retryLabel?: string;
  readonly role?: 'alert' | 'status';
}) {
  return (
    <div role={role} className="grid flex-1 place-items-center p-8 text-center">
      {onRetry === undefined ? (
        <p className={muted ? 'text-sm text-text-muted' : 'max-w-md text-[15.5px] text-danger'}>
          {message}
        </p>
      ) : (
        <div className="flex max-w-md flex-col items-center gap-3">
          <p className="text-[15.5px] text-danger">{message}</p>
          <Button type="button" variant="outline" onClick={onRetry}>
            {retryLabel}
          </Button>
        </div>
      )}
    </div>
  );
}

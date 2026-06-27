/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from 'react';
import { AlertCircle, CircleSlash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';

export type BoardStatus = 'loading' | 'populated' | 'empty' | 'error' | 'error-auth';

export function isAuthError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 401';
}

export function boardStatus(opts: {
  readonly isLoading: boolean;
  readonly isError: boolean;
  readonly error: unknown;
  readonly isEmpty: boolean;
}): BoardStatus {
  if (opts.isError) {
    return isAuthError(opts.error) ? 'error-auth' : 'error';
  }
  if (opts.isLoading) {
    return 'loading';
  }
  return opts.isEmpty ? 'empty' : 'populated';
}

export interface BoardStateViewProps {
  readonly status: BoardStatus;
  readonly emptyHeading: string;
  readonly emptyBody: string;
  readonly onRetry: () => void;
  readonly children: ReactNode;
}

export function BoardStateView({
  status,
  emptyHeading,
  emptyBody,
  onRetry,
  children,
}: BoardStateViewProps) {
  const { t } = useTranslation();

  if (status === 'loading') {
    return (
      <div
        role="status"
        aria-label={t('governance.loading')}
        className="flex h-full min-h-0 flex-col gap-3 p-4"
      >
        <span className="sr-only">{t('governance.loading')}</span>
        <Skeleton className="h-12 w-full max-w-xl bg-surface-3" />
        <Skeleton className="h-12 w-full max-w-lg bg-surface-3" />
        <Skeleton className="h-12 w-full max-w-2xl bg-surface-3" />
      </div>
    );
  }

  if (status === 'error-auth') {
    return (
      <div className="grid h-full place-items-center p-6 text-center">
        <Alert variant="destructive" className="max-w-md">
          <AlertCircle data-icon="inline-start" aria-hidden="true" focusable="false" />
          <AlertDescription>{t('governance.authExpired')}</AlertDescription>
        </Alert>
      </div>
    );
  }

  if (status === 'error') {
    return (
      <div className="grid h-full place-items-center p-6 text-center">
        <Alert variant="destructive" className="max-w-md">
          <AlertCircle data-icon="inline-start" aria-hidden="true" focusable="false" />
          <AlertDescription className="gap-3">
            <p>{t('governance.error')}</p>
            <Button type="button" variant="outline" onClick={onRetry}>
              {t('governance.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  if (status === 'empty') {
    return (
      <Empty className="h-full border-0 bg-transparent">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <CircleSlash2 aria-hidden="true" focusable="false" />
          </EmptyMedia>
          <EmptyTitle role="heading" aria-level={2} className="font-display text-[22px]">
            {emptyHeading}
          </EmptyTitle>
          <EmptyDescription className="text-[15.5px]">{emptyBody}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return <>{children}</>;
}

import { type ComponentProps } from 'react';
import { cn } from '@/lib/utils';

// new-york Card on Aura tokens (bg-card = --color-surface, text-card-foreground = --color-text).
export function Card({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="card"
      className={cn(
        'flex flex-col gap-4 rounded-lg border border-border bg-card p-4 text-card-foreground',
        className,
      )}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div data-slot="card-header" className={cn('flex flex-col gap-1', className)} {...props} />
  );
}

export function CardTitle({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-title"
      className={cn('text-[15.5px] font-semibold leading-none', className)}
      {...props}
    />
  );
}

export function CardDescription({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-description"
      className={cn('text-[13px] text-muted-foreground', className)}
      {...props}
    />
  );
}

export function CardContent({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div data-slot="card-content" className={cn('flex flex-col gap-2', className)} {...props} />
  );
}

export function CardFooter({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div data-slot="card-footer" className={cn('flex items-center gap-2', className)} {...props} />
  );
}

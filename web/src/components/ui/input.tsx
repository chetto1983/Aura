import { type ComponentProps } from 'react';
import { cn } from '@/lib/utils';

// new-york Input on Aura tokens. min-h-[44px] keeps the 44px touch floor; the focus
// ring reads --color-ring through the bridge. aria-invalid switches the border to danger.
export function Input({ className, type, ...props }: ComponentProps<'input'>) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        'flex min-h-[44px] w-full rounded-md border border-input bg-surface-3 px-3 py-2 text-[13px] text-text outline-none transition-colors',
        'placeholder:text-text-faint focus-visible:ring-2 focus-visible:ring-ring',
        'disabled:cursor-not-allowed disabled:opacity-60',
        'aria-[invalid=true]:border-danger aria-[invalid=true]:focus-visible:ring-danger',
        className,
      )}
      {...props}
    />
  );
}

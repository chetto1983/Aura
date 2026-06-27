import { type ComponentProps } from 'react';
import { ChevronDownIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface NativeSelectProps extends Omit<ComponentProps<'select'>, 'size'> {
  readonly size?: 'sm' | 'default';
}

export function NativeSelect({ className, size = 'default', ...props }: NativeSelectProps) {
  return (
    <div
      data-slot="native-select-wrapper"
      className="group/native-select relative w-fit has-[select.w-full]:w-full has-[select:disabled]:opacity-60"
    >
      <select
        data-slot="native-select"
        data-size={size}
        className={cn(
          'min-h-[44px] w-full min-w-0 appearance-none rounded-md border border-input bg-surface-3 px-3 py-2 pr-9 text-[13px] text-text outline-none transition-colors',
          'focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:cursor-not-allowed',
          'aria-[invalid=true]:border-danger aria-[invalid=true]:focus-visible:ring-danger',
          size === 'sm' ? 'px-2 py-1.5 pr-8' : undefined,
          className,
        )}
        {...props}
      />
      <ChevronDownIcon
        data-slot="native-select-icon"
        aria-hidden="true"
        className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground opacity-70"
      />
    </div>
  );
}

export function NativeSelectOption({ className, ...props }: ComponentProps<'option'>) {
  return (
    <option
      data-slot="native-select-option"
      className={cn('bg-[Canvas] text-[CanvasText]', className)}
      {...props}
    />
  );
}

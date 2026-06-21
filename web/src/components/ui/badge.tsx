import { type ComponentProps } from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

// new-york Badge on Aura tokens. The status variants (warning/danger/success) use the
// soft 15%-tint-on-token pattern (bg-<status>/15 text-<status>) so a chip reads as a
// status without stealing the scarce accent fill (03-SPEC §4.3).
const badgeVariants = cva(
  'inline-flex w-fit shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 text-[13px] font-semibold [&>svg]:size-3 [&>svg]:pointer-events-none',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-primary text-primary-foreground',
        secondary: 'border-border bg-secondary text-secondary-foreground',
        warning: 'border-warning bg-warning/15 text-warning',
        danger: 'border-danger bg-danger/15 text-danger',
        success: 'border-success bg-success/15 text-success',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
);

export interface BadgeProps extends ComponentProps<'span'>, VariantProps<typeof badgeVariants> {
  readonly asChild?: boolean;
}

export function Badge({ className, variant, asChild = false, ...props }: BadgeProps) {
  const Comp = asChild ? Slot : 'span';
  return (
    <Comp data-slot="badge" className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}

export { badgeVariants };

import { type ComponentProps } from 'react';
import { DropdownMenu as DropdownMenuPrimitive } from 'radix-ui';
import { cn } from '@/lib/utils';

export function DropdownMenu({ ...props }: ComponentProps<typeof DropdownMenuPrimitive.Root>) {
  return <DropdownMenuPrimitive.Root data-slot="dropdown-menu" {...props} />;
}

export function DropdownMenuTrigger({
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Trigger>) {
  return <DropdownMenuPrimitive.Trigger data-slot="dropdown-menu-trigger" {...props} />;
}

export function DropdownMenuGroup({
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Group>) {
  return <DropdownMenuPrimitive.Group data-slot="dropdown-menu-group" {...props} />;
}

export function DropdownMenuContent({
  className,
  sideOffset = 6,
  align = 'end',
  collisionPadding = 12,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        data-slot="dropdown-menu-content"
        align={align}
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn(
          'z-[80] max-h-[min(22rem,var(--radix-dropdown-menu-content-available-height))] min-w-56 max-w-[calc(100vw-1.5rem)] overflow-y-auto rounded-md border border-border bg-surface p-1 text-text shadow-[var(--shadow-popover)]',
          'data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0 data-[state=open]:zoom-in-95 data-[state=closed]:zoom-out-95',
          'data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2',
          className,
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  );
}

export function DropdownMenuLabel({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      data-slot="dropdown-menu-label"
      className={cn('px-3 py-2 text-[12px] font-semibold text-text-muted', className)}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      data-slot="dropdown-menu-separator"
      className={cn('-mx-1 my-1 h-px bg-border', className)}
      {...props}
    />
  );
}

export interface DropdownMenuItemProps extends ComponentProps<typeof DropdownMenuPrimitive.Item> {
  readonly variant?: 'default' | 'destructive';
}

export function DropdownMenuItem({
  className,
  variant = 'default',
  ...props
}: DropdownMenuItemProps) {
  return (
    <DropdownMenuPrimitive.Item
      data-slot="dropdown-menu-item"
      data-variant={variant}
      className={cn(
        'relative flex min-h-11 cursor-default select-none items-center gap-2 rounded-sm px-3 py-2 text-[14px] outline-none transition-colors',
        'focus:bg-surface-2 data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg:not([class*="size-"])]:size-4 [&_svg]:shrink-0',
        variant === 'destructive' ? 'text-danger focus:text-danger' : 'text-text',
        className,
      )}
      {...props}
    />
  );
}

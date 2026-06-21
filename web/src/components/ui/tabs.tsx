import { type ComponentProps } from 'react';
import * as TabsPrimitive from '@radix-ui/react-tabs';
import { cn } from '@/lib/utils';

// new-york Tabs on Aura tokens. The active trigger carries the scarce accent fill
// (data-[state=active]:bg-primary) — accent only on the selected tab (03-SPEC §4.3).
export function Tabs({ className, ...props }: ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn('flex flex-col gap-4', className)}
      {...props}
    />
  );
}

export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        'inline-flex w-fit items-center gap-1 rounded-md border border-border bg-surface-2 p-1',
        className,
      )}
      {...props}
    />
  );
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        'inline-flex min-h-[44px] flex-1 items-center justify-center gap-2 rounded-md px-3 py-1.5 text-[13px] font-semibold whitespace-nowrap text-text-muted outline-none transition-colors',
        'hover:text-text focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-60',
        'data-[state=active]:bg-primary data-[state=active]:text-primary-foreground',
        "[&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4 [&_svg]:shrink-0",
        className,
      )}
      {...props}
    />
  );
}

export function TabsContent({ className, ...props }: ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn('flex-1 outline-none', className)}
      {...props}
    />
  );
}

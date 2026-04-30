import { cn } from "@/lib/utils";

// Skeleton is the standard shadcn loading placeholder. Animated pulse on
// a muted background; the consumer sets width/height/shape via class.
function Skeleton({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-slot="skeleton"
      className={cn("animate-pulse rounded-md bg-muted", className)}
      {...props}
    />
  );
}

export { Skeleton };

import * as React from "react"
import { Progress as ProgressPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function Progress({
  className,
  value,
  ...props
}: React.ComponentProps<typeof ProgressPrimitive.Root>) {
  const normalizedValue = typeof value === "number" ? Math.max(0, Math.min(100, value)) : 0

  return (
    <ProgressPrimitive.Root
      data-slot="progress"
      className={cn(
        "relative flex h-1 w-full items-center overflow-x-hidden rounded-full bg-muted",
        className
      )}
      {...props}
    >
      <ProgressPrimitive.Indicator asChild data-slot="progress-indicator">
        <svg className="aura-progress__indicator" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true" focusable="false">
          <rect className="aura-progress__bar" x="0" y="0" width={normalizedValue} height="4" rx="2" />
        </svg>
      </ProgressPrimitive.Indicator>
    </ProgressPrimitive.Root>
  )
}

export { Progress }

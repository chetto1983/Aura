import type * as React from "react";
import { cn } from "@/lib/utils";

export function Skeleton({
  className,
  rounded = "md",
  ...props
}: React.ComponentProps<"div"> & { rounded?: "sm" | "md" | "lg" | "full" }) {
  const radius = {
    sm: "rounded-[var(--radius-sm)]",
    md: "rounded-[var(--radius)]",
    lg: "rounded-[var(--radius-lg)]",
    full: "rounded-full",
  }[rounded];

  return (
    <div
      aria-hidden="true"
      className={cn("skeleton bg-[var(--skeleton-base)]", radius, className)}
      {...props}
    />
  );
}

export function ToolArtifactSkeleton() {
  return (
    <div className="rounded-[var(--radius)] border border-[--border] bg-[--surface-raised] p-4">
      <div className="mb-4 flex items-center gap-3">
        <Skeleton className="size-8" rounded="full" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3 w-40" />
          <Skeleton className="h-2.5 w-24" />
        </div>
      </div>
      <div className="grid gap-2">
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-11/12" />
        <Skeleton className="h-3 w-2/3" />
      </div>
    </div>
  );
}

export function SkillsListSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="grid gap-3" role="status" aria-label="Caricamento skills">
      {Array.from({ length: rows }).map((_, index) => (
        <div
          key={index}
          className="flex flex-col gap-3 rounded-[var(--radius)] border border-[--border] bg-[--surface-raised] p-3 sm:flex-row sm:items-center sm:justify-between"
        >
          <div className="min-w-0 flex-1 space-y-2">
            <Skeleton className="h-4 w-36" />
            <Skeleton className="h-3 w-56 max-w-full" />
          </div>
          <div className="flex gap-2">
            <Skeleton className="h-11 w-24" />
            <Skeleton className="h-11 w-28" />
          </div>
        </div>
      ))}
      <span className="sr-only">Caricamento skills...</span>
    </div>
  );
}

export function CommandResultsSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="grid gap-1 p-2" role="status" aria-label="Ricerca in corso">
      {Array.from({ length: rows }).map((_, index) => (
        <div key={index} className="rounded-[var(--radius-sm)] p-2">
          <Skeleton className="mb-2 h-3.5 w-32" />
          <Skeleton className="h-3 w-full" />
        </div>
      ))}
      <span className="sr-only">Ricerca in corso...</span>
    </div>
  );
}

export function GraphSkeleton() {
  return (
    <div className="absolute inset-0 overflow-hidden bg-[--surface]" role="status" aria-label="Caricamento grafo">
      <div className="absolute left-[12%] top-[16%] size-20 rounded-full border border-[--border] bg-[--surface-raised]" />
      <div className="absolute right-[18%] top-[22%] size-14 rounded-full border border-[--border] bg-[--surface-raised]" />
      <div className="absolute bottom-[20%] left-[30%] size-16 rounded-full border border-[--border] bg-[--surface-raised]" />
      <div className="absolute bottom-[28%] right-[26%] size-10 rounded-full border border-[--border] bg-[--surface-raised]" />
      <div className="absolute left-[18%] top-[28%] h-px w-[34%] rotate-12 bg-[--border]" />
      <div className="absolute right-[28%] top-[34%] h-px w-[28%] -rotate-12 bg-[--border]" />
      <div className="absolute bottom-[34%] left-[34%] h-px w-[26%] rotate-45 bg-[--border]" />
      <div className="absolute inset-0 grid place-items-center">
        <div className="rounded-[var(--radius)] border border-[--border] bg-[--surface-raised] p-4 shadow-[var(--shadow-md)]">
          <Skeleton className="mb-3 h-4 w-40" />
          <Skeleton className="h-3 w-56" />
        </div>
      </div>
      <span className="sr-only">Caricamento grafo...</span>
    </div>
  );
}

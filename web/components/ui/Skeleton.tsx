import { cn } from "@/lib/cn";

export interface SkeletonProps {
  className?: string;
}

export function Skeleton({ className }: SkeletonProps) {
  return (
    <div
      className={cn("animate-pulse rounded-md bg-surface-muted motion-reduce:animate-none", className)}
      aria-hidden="true"
      data-testid="skeleton"
    />
  );
}

export function PlanCardSkeleton() {
  return (
    <div
      className="flex flex-col rounded-lg border border-line bg-surface p-5"
      data-testid="plan-card-skeleton"
    >
      <Skeleton className="h-6 w-3/4" />
      <div className="mt-4 space-y-2">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    </div>
  );
}

/**
 * Full-area loading placeholder for data-heavy plan pages: a KPI strip plus two
 * card blocks. Announces the loading label to AT while showing the skeleton.
 */
export function PageSkeleton({ label = "加载中…" }: { label?: string }) {
  return (
    <div role="status" aria-live="polite" data-testid="page-skeleton" className="space-y-6">
      <span className="sr-only">{label}</span>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="rounded-lg border border-line bg-surface p-4">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="mt-3 h-6 w-24" />
          </div>
        ))}
      </div>
      {Array.from({ length: 2 }, (_, i) => (
        <div key={i} className="rounded-lg border border-line bg-surface p-5">
          <Skeleton className="h-5 w-40" />
          <div className="mt-4 space-y-2">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-11/12" />
            <Skeleton className="h-4 w-4/5" />
          </div>
        </div>
      ))}
    </div>
  );
}

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

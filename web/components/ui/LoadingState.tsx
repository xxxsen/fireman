import { cn } from "@/lib/cn";

export interface LoadingStateProps {
  label?: string;
  className?: string;
}

export function LoadingState({ label = "加载中…", className }: LoadingStateProps) {
  return (
    <p
      className={cn("inline-flex items-center gap-2 text-sm text-ink-muted", className)}
      role="status"
      aria-live="polite"
      data-testid="loading-state"
    >
      <span
        aria-hidden="true"
        className="inline-block h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-line border-t-brand motion-reduce:animate-none"
      />
      {label}
    </p>
  );
}

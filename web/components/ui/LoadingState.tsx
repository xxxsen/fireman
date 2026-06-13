import { cn } from "@/lib/cn";

export interface LoadingStateProps {
  label?: string;
  className?: string;
}

export function LoadingState({ label = "加载中…", className }: LoadingStateProps) {
  return (
    <p
      className={cn("text-sm text-ink-muted", className)}
      role="status"
      aria-live="polite"
      data-testid="loading-state"
    >
      {label}
    </p>
  );
}

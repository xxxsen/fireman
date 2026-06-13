import { cn } from "@/lib/cn";
import { Button } from "./Button";

export interface ErrorStateProps {
  title?: string;
  message: string;
  onRetry?: () => void;
  retryLabel?: string;
  backHref?: string;
  backLabel?: string;
  technicalDetail?: string;
  className?: string;
}

export function ErrorState({
  title = "加载失败",
  message,
  onRetry,
  retryLabel = "重试",
  backHref,
  backLabel = "返回",
  technicalDetail,
  className,
}: ErrorStateProps) {
  return (
    <div
      role="alert"
      className={cn(
        "rounded-lg border border-danger/30 bg-danger/5 px-6 py-8 text-center",
        className,
      )}
      data-testid="error-state"
    >
      <h2 className="text-base font-medium text-danger">{title}</h2>
      <p className="mx-auto mt-2 max-w-lg text-sm text-ink">{message}</p>
      {technicalDetail && (
        <p className="mx-auto mt-2 max-w-lg font-mono-numeric text-xs text-ink-muted">
          {technicalDetail}
        </p>
      )}
      <div className="mt-5 flex flex-wrap items-center justify-center gap-3">
        {onRetry && (
          <Button onClick={onRetry} data-testid="error-state-retry">
            {retryLabel}
          </Button>
        )}
        {backHref && (
          <Button href={backHref} variant="secondary" data-testid="error-state-back">
            {backLabel}
          </Button>
        )}
      </div>
    </div>
  );
}

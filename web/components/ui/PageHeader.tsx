import Link from "next/link";
import { cn } from "@/lib/cn";
import { Button } from "./Button";

export interface PageHeaderProps {
  backHref?: string;
  backLabel?: string;
  eyebrow?: string;
  title: string;
  description?: string;
  status?: React.ReactNode;
  secondaryActions?: React.ReactNode;
  primaryAction?: {
    label: string;
    href?: string;
    onClick?: () => void;
    pending?: boolean;
    disabled?: boolean;
  };
  className?: string;
}

export function PageHeader({
  backHref,
  backLabel = "返回",
  eyebrow,
  title,
  description,
  status,
  secondaryActions,
  primaryAction,
  className,
}: PageHeaderProps) {
  return (
    <header className={cn("mb-6 space-y-3", className)} data-testid="page-header">
      {backHref && (
        <Link
          href={backHref}
          className="inline-flex text-sm text-ink-muted underline-offset-2 hover:text-ink hover:underline"
        >
          ← {backLabel}
        </Link>
      )}

      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 flex-1 space-y-1">
          {eyebrow && (
            <p className="text-xs font-medium uppercase tracking-wide text-ink-muted">{eyebrow}</p>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold text-ink">{title}</h1>
            {status}
          </div>
          {description && <p className="max-w-2xl text-sm text-ink-muted">{description}</p>}
        </div>

        {(secondaryActions || primaryAction) && (
          <div
            className="flex shrink-0 flex-col gap-2 sm:flex-row sm:items-center"
            data-testid="page-header-actions"
          >
            {secondaryActions && (
              <div className="flex flex-wrap items-center gap-2">{secondaryActions}</div>
            )}
            {primaryAction &&
              (primaryAction.href ? (
                <Button
                  href={primaryAction.href}
                  pending={primaryAction.pending}
                  disabled={primaryAction.disabled}
                  data-testid="page-header-primary"
                >
                  {primaryAction.label}
                </Button>
              ) : (
                <Button
                  onClick={primaryAction.onClick}
                  pending={primaryAction.pending}
                  disabled={primaryAction.disabled}
                  data-testid="page-header-primary"
                >
                  {primaryAction.label}
                </Button>
              ))}
          </div>
        )}
      </div>
    </header>
  );
}

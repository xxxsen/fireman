import { cn } from "@/lib/cn";
import { Button } from "./Button";

export interface EmptyStateProps {
  title: string;
  description?: string;
  action?: {
    label: string;
    href?: string;
    onClick?: () => void;
  };
  className?: string;
}

export function EmptyState({ title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        "rounded-lg border border-dashed border-line bg-surface px-6 py-10 text-center",
        className,
      )}
      data-testid="empty-state"
    >
      <h2 className="text-base font-medium text-ink">{title}</h2>
      {description && <p className="mx-auto mt-2 max-w-md text-sm text-ink-muted">{description}</p>}
      {action &&
        (action.href ? (
          <Button href={action.href} className="mt-5" data-testid="empty-state-action">
            {action.label}
          </Button>
        ) : (
          <Button onClick={action.onClick} className="mt-5" data-testid="empty-state-action">
            {action.label}
          </Button>
        ))}
    </div>
  );
}

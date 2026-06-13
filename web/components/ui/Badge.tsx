import { cn } from "@/lib/cn";

export type BadgeVariant = "neutral" | "positive" | "warning" | "danger" | "info";

const VARIANT_CLASSES: Record<BadgeVariant, string> = {
  neutral: "border-line bg-surface-muted text-ink-muted",
  positive: "border-positive/25 bg-positive/10 text-positive",
  warning: "border-warning/30 bg-warning/10 text-warning",
  danger: "border-danger/30 bg-danger/10 text-danger",
  info: "border-info/25 bg-info/10 text-info",
};

export interface BadgeProps {
  variant?: BadgeVariant;
  children: React.ReactNode;
  className?: string;
}

export function Badge({ variant = "neutral", children, className }: BadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium",
        VARIANT_CLASSES[variant],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function instrumentStatusBadgeVariant(status: string): BadgeVariant {
  switch (status) {
    case "pending_fetch":
    case "pending_sync":
      return "info";
    case "fetch_failed":
    case "classification_failed":
    case "data_anomaly":
      return "danger";
    case "insufficient_history":
      return "warning";
    case "available":
    case "active":
      return "positive";
    default:
      return "neutral";
  }
}

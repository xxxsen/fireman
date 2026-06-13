import { cn } from "@/lib/cn";

export type AlertVariant = "info" | "warning" | "danger" | "success";

const VARIANT_CLASSES: Record<AlertVariant, string> = {
  info: "border-info/25 bg-info/5 text-info",
  warning: "border-warning/30 bg-warning/5 text-warning",
  danger: "border-danger/30 bg-danger/5 text-danger",
  success: "border-positive/30 bg-positive/5 text-positive",
};

export interface AlertProps {
  variant?: AlertVariant;
  title?: string;
  children: React.ReactNode;
  className?: string;
}

export function Alert({ variant = "info", title, children, className }: AlertProps) {
  return (
    <div
      role="alert"
      className={cn("rounded-lg border px-4 py-3 text-sm", VARIANT_CLASSES[variant], className)}
    >
      {title && <p className="mb-1 font-medium">{title}</p>}
      <div>{children}</div>
    </div>
  );
}

import Link from "next/link";
import { cn } from "@/lib/cn";

export type StatCardTone = "normal" | "warning" | "danger";

const TONE_BAR: Record<StatCardTone, string> = {
  normal: "bg-brand",
  warning: "bg-warning",
  danger: "bg-danger",
};

export interface StatCardProps {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  tone?: StatCardTone;
  /** When set the whole card links to a pre-filtered board. */
  href?: string;
  className?: string;
}

/**
 * Overview stat card: big number + label + a 4px semantic tone bar. Abnormal
 * numbers link straight to the pre-filtered board so "seeing" and "locating"
 * an anomaly is one click apart.
 */
export function StatCard({ label, value, hint, tone = "normal", href, className }: StatCardProps) {
  const body = (
    <div
      className={cn(
        "relative flex h-full flex-col justify-between overflow-hidden rounded-lg border border-line bg-surface p-4 pl-5",
        href && "transition-colors hover:border-brand/40 hover:bg-surface-muted",
        className,
      )}
      data-testid="stat-card"
    >
      <span className={cn("absolute inset-y-0 left-0 w-1", TONE_BAR[tone])} aria-hidden="true" />
      <p className="text-xs text-ink-muted">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums text-ink">{value}</p>
      {hint && <p className="mt-1 text-xs text-ink-muted">{hint}</p>}
    </div>
  );

  if (href) {
    return (
      <Link href={href} className="block h-full" data-testid="stat-card-link">
        {body}
      </Link>
    );
  }
  return body;
}

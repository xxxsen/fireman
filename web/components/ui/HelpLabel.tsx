import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { MetricHelp } from "./MetricHelp";

export interface HelpLabelProps {
  label: ReactNode;
  termKey: string;
  className?: string;
  helpClassName?: string;
}

/** Keeps a business label and its contextual-help trigger together. */
export function HelpLabel({ label, termKey, className, helpClassName }: HelpLabelProps) {
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-0.5", className)}>
      <span>{label}</span>
      <MetricHelp termKey={termKey} className={helpClassName} />
    </span>
  );
}

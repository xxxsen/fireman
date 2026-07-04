"use client";

import type { ReactNode } from "react";
import { helpForTerm, labelForTerm } from "@/lib/terms";
import { Tooltip } from "./Tooltip";

export interface MetricHelpProps {
  termKey?: string;
  text?: string;
  label?: ReactNode;
  className?: string;
}

export function MetricHelp({ termKey, text, label, className }: MetricHelpProps) {
  const helpText = text ?? helpForTerm(termKey);
  const termLabel = labelForTerm(termKey);

  if (!helpText) return null;

  return (
    <Tooltip
      content={helpText}
      align="center"
      clickToggle
      followCursor
      triggerTestId="metric-help-trigger"
      contentTestId="metric-help-tooltip"
      contentClassName="w-60"
    >
      <button
        type="button"
        aria-label={termLabel ? `查看「${termLabel}」说明` : "查看说明"}
        className={
          className ??
          "ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full border border-line bg-surface text-[10px] text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink"
        }
      >
        {label ?? "?"}
      </button>
    </Tooltip>
  );
}

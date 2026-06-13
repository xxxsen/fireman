"use client";

import type { ReactNode } from "react";
import { helpForTerm } from "@/lib/terms";
import { Tooltip } from "./Tooltip";

export interface MetricHelpProps {
  termKey?: string;
  text?: string;
  label?: ReactNode;
  className?: string;
}

export function MetricHelp({ termKey, text, label, className }: MetricHelpProps) {
  const helpText = text ?? helpForTerm(termKey);

  if (!helpText) return null;

  return (
    <Tooltip
      content={helpText}
      align="center"
      clickToggle
      triggerTestId="metric-help-trigger"
      contentTestId="metric-help-tooltip"
      contentClassName="w-60"
    >
      <button
        type="button"
        aria-label="查看说明"
        className={
          className ??
          "ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full border border-slate-300 bg-white text-[10px] text-slate-600 hover:bg-slate-50"
        }
      >
        {label ?? "?"}
      </button>
    </Tooltip>
  );
}

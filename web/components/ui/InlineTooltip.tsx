"use client";

import type { ReactNode } from "react";
import { Tooltip } from "./Tooltip";

export interface InlineTooltipProps {
  content: string;
  children: ReactNode;
  className?: string;
}

export function InlineTooltip({ content, children, className }: InlineTooltipProps) {
  return (
    <Tooltip
      content={content}
      align="end"
      className={className}
      triggerTestId="inline-tooltip-trigger"
      contentTestId="inline-tooltip-content"
      contentClassName="w-64 whitespace-pre-line text-left"
    >
      <span
        role="button"
        tabIndex={0}
        className="cursor-help underline decoration-dotted decoration-line underline-offset-2"
      >
        {children}
      </span>
    </Tooltip>
  );
}

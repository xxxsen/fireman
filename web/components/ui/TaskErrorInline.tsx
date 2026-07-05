"use client";

import { cn } from "@/lib/cn";
import { Tooltip } from "./Tooltip";

const MAX_INLINE_CHARS = 80;

export interface TaskErrorInlineProps {
  errorCode?: string;
  errorMessage?: string;
  className?: string;
}

/**
 * Short inline error text (truncated at 80 chars) with the full error shown in
 * a tooltip on hover or keyboard focus.
 */
export function TaskErrorInline({ errorCode, errorMessage, className }: TaskErrorInlineProps) {
  const message = (errorMessage ?? "").trim() || (errorCode ?? "").trim();
  if (!message) return null;

  const truncated =
    message.length > MAX_INLINE_CHARS ? `${message.slice(0, MAX_INLINE_CHARS)}…` : message;
  const full = errorCode && errorMessage ? `${errorCode}: ${errorMessage}` : message;

  return (
    <Tooltip
      content={full}
      align="end"
      // The tooltip wrapper is a flex/grid child at both call sites; without
      // min-w-0 its implicit min-width:auto lets long errors overflow the
      // container instead of truncating.
      className={cn("min-w-0 max-w-full overflow-hidden", className)}
      triggerTestId="task-error-inline"
      contentTestId="task-error-tooltip"
      contentClassName="max-w-sm whitespace-pre-line break-words text-left"
    >
      <span
        role="button"
        tabIndex={0}
        className="block min-w-0 max-w-full cursor-help truncate text-xs text-danger underline decoration-dotted decoration-danger/50 underline-offset-2"
      >
        {truncated}
      </span>
    </Tooltip>
  );
}

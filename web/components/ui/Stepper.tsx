"use client";

import { cn } from "@/lib/cn";

export interface StepperProps {
  steps: readonly string[];
  current: number;
  className?: string;
}

/**
 * Shared wizard step indicator: an ordered list of pills with a completed
 * check, the active step highlighted and `aria-current="step"` for AT.
 */
export function Stepper({ steps, current, className }: StepperProps) {
  return (
    <ol
      className={cn("flex flex-wrap gap-2 text-sm", className)}
      data-testid="stepper"
      aria-label="步骤"
    >
      {steps.map((label, index) => {
        const state = index < current ? "done" : index === current ? "current" : "todo";
        return (
          <li
            key={label}
            aria-current={state === "current" ? "step" : undefined}
            className={cn(
              "rounded-full px-3 py-1",
              state === "current" && "bg-brand font-medium text-surface",
              state === "done" && "bg-brand/10 text-brand-strong",
              state === "todo" && "bg-surface-muted text-ink-muted",
            )}
          >
            {state === "done" ? `✓ ${label}` : `${index + 1}. ${label}`}
          </li>
        );
      })}
    </ol>
  );
}

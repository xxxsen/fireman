"use client";

import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";

export interface InlineTooltipProps {
  content: string;
  children: ReactNode;
  className?: string;
}

export function InlineTooltip({ content, children, className }: InlineTooltipProps) {
  const [open, setOpen] = useState(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tooltipId = useId();

  const cancelClose = useCallback(() => {
    if (closeTimer.current) {
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }, []);

  const scheduleClose = useCallback(() => {
    cancelClose();
    closeTimer.current = setTimeout(() => setOpen(false), 200);
  }, [cancelClose]);

  useEffect(() => () => cancelClose(), [cancelClose]);

  return (
    <span className={`relative inline-flex ${className ?? ""}`}>
      <span
        role="button"
        tabIndex={0}
        aria-describedby={open ? tooltipId : undefined}
        data-testid="inline-tooltip-trigger"
        onMouseEnter={() => {
          cancelClose();
          setOpen(true);
        }}
        onMouseLeave={scheduleClose}
        onFocus={() => {
          cancelClose();
          setOpen(true);
        }}
        onBlur={scheduleClose}
        className="cursor-help underline decoration-dotted decoration-slate-300 underline-offset-2"
      >
        {children}
      </span>
      {open && (
        <span
          role="tooltip"
          id={tooltipId}
          data-testid="inline-tooltip-content"
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
          className="absolute right-0 top-full z-30 mt-1 w-64 whitespace-pre-line rounded-lg border border-slate-200 bg-white p-2 text-left text-xs leading-relaxed text-slate-700 shadow-lg"
        >
          {content}
        </span>
      )}
    </span>
  );
}

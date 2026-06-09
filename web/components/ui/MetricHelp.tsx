"use client";

import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { helpForTerm } from "@/lib/terms";

export interface MetricHelpProps {
  termKey?: string;
  text?: string;
  label?: ReactNode;
  className?: string;
}

export function MetricHelp({ termKey, text, label, className }: MetricHelpProps) {
  const helpText = text ?? helpForTerm(termKey);
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

  if (!helpText) return null;

  return (
    <span className="relative inline-flex">
      <button
        type="button"
        aria-label="查看说明"
        aria-describedby={open ? tooltipId : undefined}
        aria-expanded={open}
        data-testid="metric-help-trigger"
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
        onClick={() => setOpen((v) => !v)}
        className={
          className ??
          "ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full border border-slate-300 bg-white text-[10px] text-slate-600 hover:bg-slate-50"
        }
      >
        {label ?? "?"}
      </button>
      {open && (
        <span
          role="tooltip"
          id={tooltipId}
          data-testid="metric-help-tooltip"
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
          className="absolute left-1/2 top-full z-30 mt-1 w-60 -translate-x-1/2 rounded-lg border border-slate-200 bg-white p-2 text-xs leading-relaxed text-slate-700 shadow-lg"
        >
          {helpText}
        </span>
      )}
    </span>
  );
}

"use client";

import {
  Children,
  cloneElement,
  isValidElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactElement,
  type ReactNode,
} from "react";
import { cn } from "@/lib/cn";
import { computeTooltipPosition, TOOLTIP_VIEWPORT_PADDING, type TooltipAlign } from "@/lib/tooltip-position";

const CLOSE_DELAY_MS = 200;

export interface TooltipProps {
  content: ReactNode;
  children: ReactElement;
  align?: TooltipAlign;
  className?: string;
  contentClassName?: string;
  triggerTestId?: string;
  contentTestId?: string;
  clickToggle?: boolean;
}

export function Tooltip({
  content,
  children,
  align = "center",
  className,
  contentClassName,
  triggerTestId,
  contentTestId,
  clickToggle = false,
}: TooltipProps) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<CSSProperties | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const triggerRef = useRef<HTMLSpanElement | null>(null);
  const tooltipRef = useRef<HTMLSpanElement | null>(null);
  const tooltipId = useId();

  const cancelClose = useCallback(() => {
    if (closeTimer.current) {
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }, []);

  const scheduleClose = useCallback(() => {
    cancelClose();
    closeTimer.current = setTimeout(() => setOpen(false), CLOSE_DELAY_MS);
  }, [cancelClose]);

  const show = useCallback(() => {
    cancelClose();
    setOpen(true);
  }, [cancelClose]);

  useEffect(() => () => cancelClose(), [cancelClose]);

  useLayoutEffect(() => {
    if (!open || !triggerRef.current || !tooltipRef.current) {
      setPosition(null);
      return;
    }

    const triggerRect = triggerRef.current.getBoundingClientRect();
    const tooltipRect = tooltipRef.current.getBoundingClientRect();
    const next = computeTooltipPosition(
      triggerRect,
      tooltipRect,
      window.innerWidth,
      align,
      TOOLTIP_VIEWPORT_PADDING,
      window.innerHeight,
    );

    setPosition({
      position: "fixed",
      top: next.top,
      left: next.left,
      zIndex: 30,
    });
  }, [open, align, content]);

  if (!isValidElement(children)) {
    throw new Error("Tooltip requires a single React element child");
  }

  const child = Children.only(children);
  const childProps = child.props as React.HTMLAttributes<HTMLElement>;

  const triggerProps: React.HTMLAttributes<HTMLElement> & {
    "data-testid"?: string;
    "aria-expanded"?: boolean;
  } = {
    ...(triggerTestId ? { "data-testid": triggerTestId } : {}),
    "aria-describedby": open ? tooltipId : undefined,
    ...(clickToggle ? { "aria-expanded": open } : {}),
  };

  if (clickToggle) {
    triggerProps.onClick = (event: React.MouseEvent<HTMLElement>) => {
      childProps.onClick?.(event);
      setOpen((value) => !value);
    };
  }

  const trigger = cloneElement(child, triggerProps);

  return (
    <span
      ref={triggerRef}
      className={cn("relative inline-flex", className)}
      onMouseEnter={show}
      onMouseLeave={scheduleClose}
      onFocus={show}
      onBlur={scheduleClose}
    >
      {trigger}
      {open ? (
        <span
          ref={tooltipRef}
          role="tooltip"
          id={tooltipId}
          data-testid={contentTestId}
          style={{
            position: "fixed",
            top: position?.top ?? 0,
            left: position?.left ?? 0,
            visibility: position ? "visible" : "hidden",
            zIndex: 30,
          }}
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
          className={cn(
            "rounded-lg border border-line bg-surface p-2 text-xs leading-relaxed text-ink shadow-lg",
            contentClassName,
          )}
        >
          {content}
        </span>
      ) : null}
    </span>
  );
}

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
import {
  computeCursorTooltipPosition,
  computeTooltipPosition,
  TOOLTIP_VIEWPORT_PADDING,
  type TooltipAlign,
  type TooltipPoint,
} from "@/lib/tooltip-position";

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
  followCursor?: boolean;
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
  followCursor = false,
}: TooltipProps) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<CSSProperties | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const triggerRef = useRef<HTMLSpanElement | null>(null);
  const tooltipRef = useRef<HTMLSpanElement | null>(null);
  const cursorRef = useRef<TooltipPoint | null>(null);
  const openedByPointerRef = useRef(false);
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

    const tooltipRect = tooltipRef.current.getBoundingClientRect();
    const usePointer = followCursor && openedByPointerRef.current && cursorRef.current !== null;
    const next = usePointer
      ? computeCursorTooltipPosition(
          cursorRef.current as TooltipPoint,
          tooltipRect,
          window.innerWidth,
          window.innerHeight,
          TOOLTIP_VIEWPORT_PADDING,
        )
      : computeTooltipPosition(
          triggerRef.current.getBoundingClientRect(),
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
  }, [open, align, content, followCursor]);

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

  // The wrapper's pointer handlers capture the latest cursor before any click,
  // so the click toggle itself never needs to touch refs during render.
  const handlePointer = (event: React.MouseEvent<HTMLElement>) => {
    if (followCursor) {
      cursorRef.current = { x: event.clientX, y: event.clientY };
      openedByPointerRef.current = true;
    }
  };

  return (
    <span
      ref={triggerRef}
      className={cn("relative inline-flex", className)}
      onMouseEnter={(event) => {
        handlePointer(event);
        show();
      }}
      onMouseMove={handlePointer}
      onMouseLeave={scheduleClose}
      onFocus={() => {
        openedByPointerRef.current = false;
        show();
      }}
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

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
import { createPortal } from "react-dom";
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
  const [cursor, setCursor] = useState<TooltipPoint | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const triggerRef = useRef<HTMLSpanElement | null>(null);
  const tooltipRef = useRef<HTMLSpanElement | null>(null);
  const openedByPointerRef = useRef(false);
  const [pointerDownOpen, setPointerDownOpen] = useState<boolean | null>(null);
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

  const hide = useCallback(
    (restoreFocus = false) => {
      cancelClose();
      setOpen(false);
      if (restoreFocus) {
        const restore = () => {
          const trigger = triggerRef.current?.firstElementChild;
          if (trigger instanceof HTMLElement) trigger.focus();
        };
        if (typeof window.requestAnimationFrame === "function") {
          window.requestAnimationFrame(restore);
        } else {
          restore();
        }
      }
    },
    [cancelClose],
  );

  useEffect(() => () => cancelClose(), [cancelClose]);

  useEffect(() => {
    if (!open || !clickToggle) return;

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (triggerRef.current?.contains(target) || tooltipRef.current?.contains(target)) return;
      hide();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      hide(true);
    };

    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [clickToggle, hide, open]);

  useLayoutEffect(() => {
    if (!open || !triggerRef.current || !tooltipRef.current) {
      setPosition(null);
      return;
    }

    const tooltipRect = tooltipRef.current.getBoundingClientRect();
    let next: { top: number; left: number };
    if (followCursor && openedByPointerRef.current && cursor) {
      next = computeCursorTooltipPosition(
        cursor,
        tooltipRect,
        window.innerWidth,
        window.innerHeight,
        TOOLTIP_VIEWPORT_PADDING,
      );
    } else {
      next = computeTooltipPosition(
        triggerRef.current.getBoundingClientRect(),
        tooltipRect,
        window.innerWidth,
        align,
        TOOLTIP_VIEWPORT_PADDING,
        window.innerHeight,
      );
    }

    setPosition({
      position: "fixed",
      top: next.top,
      left: next.left,
      zIndex: 50,
    });
  }, [open, align, content, followCursor, cursor]);

  if (!isValidElement(children)) {
    throw new Error("Tooltip requires a single React element child");
  }

  const child = Children.only(children);
  const childProps = child.props as React.HTMLAttributes<HTMLElement>;

  const handleTriggerPointerDown = (event: React.PointerEvent<HTMLElement>) => {
    childProps.onPointerDown?.(event);
    setPointerDownOpen(open);
  };

  const handleTriggerClick = (event: React.MouseEvent<HTMLElement>) => {
    childProps.onClick?.(event);
    const wasOpenAtPointerDown = pointerDownOpen;
    setPointerDownOpen(null);
    // Touch and mouse activation focus the button before click. Preserve the
    // pre-focus state so a closed tooltip opens instead of immediately
    // toggling closed. Keyboard activation toggles the focus-open state.
    setOpen((value) =>
      wasOpenAtPointerDown === null ? !value : !wasOpenAtPointerDown,
    );
  };

  const triggerProps: React.HTMLAttributes<HTMLElement> & {
    "data-testid"?: string;
    "aria-expanded"?: boolean;
  } = {
    ...(triggerTestId ? { "data-testid": triggerTestId } : {}),
    "aria-describedby": open ? tooltipId : undefined,
    ...(clickToggle ? { "aria-expanded": open } : {}),
    ...(clickToggle
      ? {
          onPointerDown: handleTriggerPointerDown,
          onClick: handleTriggerClick,
        }
      : {}),
  };

  const trigger = cloneElement(child, triggerProps);

  // In followCursor mode, hover position is tracked in state so the tooltip
  // re-positions as the mouse moves. Keyboard focus resets to trigger-anchored.
  const handlePointer = (event: React.MouseEvent<HTMLElement>) => {
    if (followCursor) {
      openedByPointerRef.current = true;
      setCursor({ x: event.clientX, y: event.clientY });
    }
  };

  const overlay = open ? (
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
        zIndex: 50,
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
  ) : null;

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
        setCursor(null);
        show();
      }}
      onBlur={scheduleClose}
    >
      {trigger}
      {/* Render the overlay in a body portal so transformed/overflow-clipped
          ancestors cannot distort its fixed-position coordinates. */}
      {overlay && typeof document !== "undefined"
        ? createPortal(overlay, document.body)
        : overlay}
    </span>
  );
}

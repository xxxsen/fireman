export const TOOLTIP_VIEWPORT_PADDING = 8;
const TOOLTIP_OFFSET_Y = 4;
// Gap between the cursor and a follow-cursor tooltip.
export const TOOLTIP_CURSOR_OFFSET = 12;

export interface TooltipRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

export interface TooltipPoint {
  x: number;
  y: number;
}

export type TooltipAlign = "center" | "start" | "end";

// computeCursorTooltipPosition places a tooltip near the mouse, defaulting to the
// bottom-right of the cursor and flipping to the left/top when it would overflow.
export function computeCursorTooltipPosition(
  cursor: TooltipPoint,
  tooltip: TooltipRect,
  viewportWidth: number,
  viewportHeight: number,
  padding = TOOLTIP_VIEWPORT_PADDING,
): { top: number; left: number } {
  let left = cursor.x + TOOLTIP_CURSOR_OFFSET;
  if (left + tooltip.width > viewportWidth - padding) {
    left = cursor.x - TOOLTIP_CURSOR_OFFSET - tooltip.width;
  }
  if (left < padding) {
    left = padding;
  }
  if (left + tooltip.width > viewportWidth - padding) {
    left = Math.max(padding, viewportWidth - padding - tooltip.width);
  }

  let top = cursor.y + TOOLTIP_CURSOR_OFFSET;
  if (top + tooltip.height > viewportHeight - padding) {
    top = cursor.y - TOOLTIP_CURSOR_OFFSET - tooltip.height;
  }
  if (top < padding) {
    top = padding;
  }
  if (top + tooltip.height > viewportHeight - padding) {
    top = Math.max(padding, viewportHeight - padding - tooltip.height);
  }

  return { top, left };
}

export function computeTooltipPosition(
  trigger: TooltipRect,
  tooltip: TooltipRect,
  viewportWidth: number,
  align: TooltipAlign = "center",
  padding = TOOLTIP_VIEWPORT_PADDING,
  viewportHeight?: number,
): { top: number; left: number } {
  let top = trigger.top + trigger.height + TOOLTIP_OFFSET_Y;

  if (viewportHeight !== undefined) {
    if (top + tooltip.height > viewportHeight - padding) {
      const aboveTop = trigger.top - tooltip.height - TOOLTIP_OFFSET_Y;
      if (aboveTop >= padding) {
        top = aboveTop;
      } else {
        top = Math.max(padding, viewportHeight - padding - tooltip.height);
      }
    }
  }

  let left: number;
  switch (align) {
    case "start":
      left = trigger.left;
      break;
    case "end":
      left = trigger.left + trigger.width - tooltip.width;
      break;
    case "center":
    default:
      left = trigger.left + trigger.width / 2 - tooltip.width / 2;
  }

  if (left < padding) {
    left = padding;
  }
  if (left + tooltip.width > viewportWidth - padding) {
    left = Math.max(padding, viewportWidth - padding - tooltip.width);
  }

  return { top, left };
}

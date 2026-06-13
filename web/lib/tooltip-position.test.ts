import { describe, expect, it } from "vitest";
import { computeTooltipPosition, TOOLTIP_VIEWPORT_PADDING } from "./tooltip-position";

describe("computeTooltipPosition", () => {
  const tooltip = { top: 0, left: 0, width: 240, height: 40 };

  it("keeps centered tooltip inside a 390px viewport", () => {
    const trigger = { top: 100, left: 10, width: 16, height: 16 };
    const pos = computeTooltipPosition(trigger, tooltip, 390, "center");

    expect(pos.left).toBeGreaterThanOrEqual(TOOLTIP_VIEWPORT_PADDING);
    expect(pos.left + tooltip.width).toBeLessThanOrEqual(390 - TOOLTIP_VIEWPORT_PADDING);
  });

  it("shifts end-aligned tooltip away from the left edge", () => {
    const trigger = { top: 100, left: 0, width: 80, height: 20 };
    const pos = computeTooltipPosition(trigger, tooltip, 390, "end");

    expect(pos.left).toBeGreaterThanOrEqual(TOOLTIP_VIEWPORT_PADDING);
  });

  it("shifts centered tooltip away from the right edge", () => {
    const trigger = { top: 100, left: 350, width: 40, height: 20 };
    const pos = computeTooltipPosition(trigger, tooltip, 390, "center");

    expect(pos.left + tooltip.width).toBeLessThanOrEqual(390 - TOOLTIP_VIEWPORT_PADDING);
  });

  it("flips tooltip above trigger when bottom space is insufficient in a 390px viewport", () => {
    const trigger = { top: 350, left: 100, width: 16, height: 16 };
    const pos = computeTooltipPosition(trigger, tooltip, 390, "center", TOOLTIP_VIEWPORT_PADDING, 390);

    expect(pos.top).toBeGreaterThanOrEqual(TOOLTIP_VIEWPORT_PADDING);
    expect(pos.top + tooltip.height).toBeLessThanOrEqual(390 - TOOLTIP_VIEWPORT_PADDING);
    expect(pos.top + tooltip.height + 4).toBeLessThanOrEqual(trigger.top);
  });
});

import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TaskTimeline } from "./TaskTimeline";
import type { AdminTaskTimelinePhase } from "@/lib/api/admin";

const t0 = Date.parse("2026-07-01T08:00:00Z");

describe("TaskTimeline", () => {
  it("shows the interval from the previous phase", () => {
    const timeline: AdminTaskTimelinePhase[] = [
      { phase: "created", at: t0 },
      { phase: "started", at: t0 + 1500 },
    ];
    render(<TaskTimeline timeline={timeline} />);

    const list = screen.getByTestId("task-timeline");
    expect(within(list).getByText("+2s")).toBeInTheDocument();
  });

  it("hides the interval when the clock goes backwards between phases", () => {
    // Server clocks can skew; a later phase recorded "earlier" must not
    // render a negative duration like "+-3s".
    const timeline: AdminTaskTimelinePhase[] = [
      { phase: "created", at: t0 },
      { phase: "started", at: t0 - 3000 },
    ];
    render(<TaskTimeline timeline={timeline} />);

    const list = screen.getByTestId("task-timeline");
    expect(within(list).queryByText(/^\+/)).not.toBeInTheDocument();
    expect(within(list).getByText("开始执行")).toBeInTheDocument();
  });
});

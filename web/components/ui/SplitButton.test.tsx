import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SplitButton } from "./SplitButton";

function renderSplit(
  overrides: Partial<React.ComponentProps<typeof SplitButton>> = {},
) {
  const onMain = vi.fn();
  const onItem = vi.fn();
  render(
    <SplitButton
      data-testid="split"
      onMain={onMain}
      onItem={onItem}
      items={[
        { key: "a", label: "同步 A" },
        { key: "b", label: "同步 B", disabled: true, note: "同步中" },
      ]}
      {...overrides}
    >
      同步全部
    </SplitButton>,
  );
  return { onMain, onItem };
}

describe("SplitButton", () => {
  it("fires the main action and keeps the menu closed", () => {
    const { onMain, onItem } = renderSplit();
    fireEvent.click(screen.getByTestId("split-main"));
    expect(onMain).toHaveBeenCalledTimes(1);
    expect(onItem).not.toHaveBeenCalled();
    expect(screen.queryByTestId("split-menu")).not.toBeInTheDocument();
  });

  it("opens the menu, fires the item action and closes afterwards", () => {
    const { onItem } = renderSplit();
    fireEvent.click(screen.getByTestId("split-toggle"));
    expect(screen.getByTestId("split-menu")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("split-item-a"));
    expect(onItem).toHaveBeenCalledWith("a");
    expect(screen.queryByTestId("split-menu")).not.toBeInTheDocument();
  });

  it("renders disabled items with their note and blocks clicks", () => {
    const { onItem } = renderSplit();
    fireEvent.click(screen.getByTestId("split-toggle"));

    const item = screen.getByTestId("split-item-b");
    expect(item).toBeDisabled();
    expect(item).toHaveTextContent("同步中");
    fireEvent.click(item);
    expect(onItem).not.toHaveBeenCalled();
  });

  it("closes the menu on Escape and on outside click", () => {
    renderSplit();
    fireEvent.click(screen.getByTestId("split-toggle"));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("split-menu")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("split-toggle"));
    fireEvent.mouseDown(document.body);
    expect(screen.queryByTestId("split-menu")).not.toBeInTheDocument();
  });

  it("disables both halves while pending and shows progress text", () => {
    const { onMain } = renderSplit({ pending: true });
    const main = screen.getByTestId("split-main");
    expect(main).toBeDisabled();
    expect(main).toHaveTextContent("处理中…");
    expect(screen.getByTestId("split-toggle")).toBeDisabled();
    fireEvent.click(main);
    expect(onMain).not.toHaveBeenCalled();
  });
});

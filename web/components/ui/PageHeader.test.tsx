import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageHeader } from "./PageHeader";

describe("PageHeader", () => {
  it("renders title and description", () => {
    render(
      <PageHeader
        title="我的 FIRE 计划"
        description="管理所有计划"
        eyebrow="计划"
      />,
    );
    expect(screen.getByRole("heading", { level: 1, name: "我的 FIRE 计划" })).toBeInTheDocument();
    expect(screen.getByText("管理所有计划")).toBeInTheDocument();
    expect(screen.getByText("计划")).toBeInTheDocument();
  });

  it("exposes a single primary action", () => {
    render(
      <PageHeader
        title="资产资料库"
        primaryAction={{ label: "录入资产", href: "/assets/import" }}
        secondaryActions={<button type="button">导出</button>}
      />,
    );
    const primary = screen.getByTestId("page-header-primary");
    expect(primary).toHaveTextContent("录入资产");
    expect(primary).toHaveAttribute("href", "/assets/import");
    expect(screen.getAllByTestId("page-header-primary")).toHaveLength(1);
  });

  it("disabled href primary action is not tab focusable", () => {
    render(
      <PageHeader
        title="资产资料库"
        primaryAction={{ label: "录入资产", href: "/assets/import", disabled: true }}
      />,
    );
    const primary = screen.getByTestId("page-header-primary");
    expect(primary).toHaveAttribute("tabindex", "-1");
    expect(primary).toHaveAttribute("aria-disabled", "true");
  });
});

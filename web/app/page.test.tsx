import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import HomePage from "./page";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isLoading: false, error: null }),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
}));

describe("HomePage", () => {
  it("renders plan list heading", () => {
    render(<HomePage />);
    expect(screen.getByRole("heading", { name: /计划列表/ })).toBeInTheDocument();
  });
});

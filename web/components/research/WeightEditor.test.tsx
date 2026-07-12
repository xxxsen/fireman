import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ResearchCollectionDetail,
  ResearchCollectionItemView,
  ResearchReadiness,
} from "@/lib/api/research";
import {
  WeightEditor,
  distributeRemainder,
  equalWeights,
  groupEqualWeights,
} from "./WeightEditor";

function item(
  id: string,
  overrides: Partial<ResearchCollectionItemView> = {},
): ResearchCollectionItemView {
  return {
    id,
    collection_id: "rc_1",
    asset_key: `CN|cn_exchange_fund|sh|${id}`,
    enabled: true,
    weight: 0.5,
    weight_locked: false,
    adjust_policy: "hfq",
    point_type: "adjusted_close",
    asset_class: "equity",
    region: "cn",
    note: "",
    sort_order: 0,
    created_at: 0,
    updated_at: 0,
    name: `资产${id}`,
    symbol: id,
    market: "cn",
    instrument_type: "cn_exchange_fund",
    instrument_type_label: "场内 ETF / LOF",
    currency: "CNY",
    listing_status: "active",
    is_cash: false,
    ...overrides,
  };
}

function detail(items: ResearchCollectionItemView[]): ResearchCollectionDetail {
  return {
    id: "rc_1",
    name: "测试集合",
    description: "",
    base_currency: "CNY",
    initial_amount_minor: 100000000,
    rebalance_policy: "monthly",
    rebalance_threshold: 0,
    start_policy: "common_intersection",
    window_start: "",
    window_end: "",
    risk_free_rate: 0,
    transaction_cost_rate: 0,
    status: "active",
    created_at: 0,
    updated_at: 0,
    tags: [],
    items,
  };
}

function readiness(
  overrides: Partial<ResearchReadiness> = {},
): ResearchReadiness {
  return {
    ready: true,
    weight_sum: 1,
    common_start: "2018-01-01",
    common_end: "2026-07-01",
    window_start: "2018-01-01",
    window_end: "2026-07-01",
    blocking_reasons: [],
    warnings: [],
    assets: [],
    data_dependencies: {
      asset_count: 2,
      fx_pairs: [],
      stale_asset_count: 0,
      missing_history_count: 0,
    },
    ...overrides,
  };
}

const noop = () => {};
const baseHandlers = {
  pending: false,
  onUpdateItem: noop,
  onDeleteItem: noop,
  onApplyWeights: noop,
  onNormalize: noop,
  onAddAsset: noop,
};

describe("weight helpers", () => {
  it("equalWeights sums exactly to 1", () => {
    const w = equalWeights(3);
    expect(w).toHaveLength(3);
    expect(w.reduce((s, v) => s + v, 0)).toBeCloseTo(1, 9);
    expect(w[0]).toBeCloseTo(1 / 3, 5);
  });

  it("groupEqualWeights splits per group then per member", () => {
    const w = groupEqualWeights([
      { id: "a", group: "equity" },
      { id: "b", group: "equity" },
      { id: "c", group: "bond" },
    ]);
    expect(w.get("a")).toBeCloseTo(0.25, 5);
    expect(w.get("b")).toBeCloseTo(0.25, 5);
    expect(w.get("c")).toBeCloseTo(0.5, 5);
    const sum = Array.from(w.values()).reduce((s, v) => s + v, 0);
    expect(sum).toBeCloseTo(1, 9);
  });

  it("distributeRemainder only touches unlocked items", () => {
    const w = distributeRemainder([
      { id: "a", weight: 0.5, locked: true },
      { id: "b", weight: 0.2, locked: false },
      { id: "c", weight: 0.1, locked: false },
    ]);
    expect(w.has("a")).toBe(false);
    expect(w.get("b")).toBeCloseTo(0.3, 6);
    expect(w.get("c")).toBeCloseTo(0.2, 6);
  });
});

describe("WeightEditor", () => {
  beforeEach(() => vi.clearAllMocks());

  it("shows an invalid weight sum with the gap", () => {
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.5 }),
          item("b", { weight: 0.3 }),
        ])}
        {...baseHandlers}
      />,
    );
    const sum = screen.getByTestId("weight-sum");
    expect(sum).toHaveTextContent("80%");
    expect(sum).toHaveTextContent("差 20%");
  });

  it("shows a valid weight sum without gap text", () => {
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.6 }),
          item("b", { weight: 0.4 }),
        ])}
        {...baseHandlers}
      />,
    );
    const sum = screen.getByTestId("weight-sum");
    expect(sum).toHaveTextContent("100%");
    expect(sum).not.toHaveTextContent("差");
  });

  it("ignores disabled items in the weight sum", () => {
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.6 }),
          item("b", { weight: 0.4 }),
          item("c", { weight: 0.9, enabled: false }),
        ])}
        {...baseHandlers}
      />,
    );
    expect(screen.getByTestId("weight-sum")).toHaveTextContent("100%");
  });

  it("commits weight edits as decimals", () => {
    const onUpdateItem = vi.fn();
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.5 }),
          item("b", { weight: 0.5 }),
        ])}
        {...baseHandlers}
        onUpdateItem={onUpdateItem}
      />,
    );
    const input = screen.getAllByLabelText("权重百分比")[0]!;
    fireEvent.change(input, { target: { value: "35" } });
    fireEvent.blur(input);
    expect(onUpdateItem).toHaveBeenCalledWith("a", { weight: 0.35 });
  });

  it("clears a zero weight draft on focus so typing replaces 0", () => {
    const onUpdateItem = vi.fn();
    render(
      <WeightEditor
        detail={detail([item("a", { weight: 0 }), item("b", { weight: 1 })])}
        {...baseHandlers}
        onUpdateItem={onUpdateItem}
      />,
    );
    const input = screen.getAllByLabelText("权重百分比")[0]!;
    fireEvent.focus(input);
    expect(input).toHaveValue("");

    fireEvent.change(input, { target: { value: "50" } });
    fireEvent.blur(input);

    expect(onUpdateItem).toHaveBeenCalledWith("a", { weight: 0.5 });
  });

  it("applies equal weights across enabled items", () => {
    const onApplyWeights = vi.fn();
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.9 }),
          item("b", { weight: 0.05 }),
          item("c", { weight: 0.05, enabled: false }),
        ])}
        {...baseHandlers}
        onApplyWeights={onApplyWeights}
      />,
    );
    fireEvent.click(screen.getByTestId("equal-weight"));
    expect(onApplyWeights).toHaveBeenCalledTimes(1);
    const updates = onApplyWeights.mock.calls[0]![0] as {
      itemId: string;
      weight: number;
    }[];
    expect(updates).toHaveLength(2);
    expect(updates.map((u) => u.itemId)).toEqual(["a", "b"]);
    expect(updates.reduce((s, u) => s + u.weight, 0)).toBeCloseTo(1, 9);
  });

  it("invokes lock-preserving normalization", () => {
    const onNormalize = vi.fn();
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.5, weight_locked: true }),
          item("b", { weight: 0.3 }),
        ])}
        {...baseHandlers}
        onNormalize={onNormalize}
      />,
    );
    fireEvent.click(screen.getByTestId("normalize-weights"));
    expect(onNormalize).toHaveBeenCalledTimes(1);
  });

  it("disables 剩余分配 when weights already valid", () => {
    render(
      <WeightEditor
        detail={detail([
          item("a", { weight: 0.5 }),
          item("b", { weight: 0.5 }),
        ])}
        {...baseHandlers}
      />,
    );
    expect(screen.getByTestId("distribute-remainder")).toBeDisabled();
  });

  it("shows per-item data status badges from readiness", () => {
    const items = [
      item("a"),
      item("b"),
      item("cash", { is_cash: true, name: "现金 CNY" }),
    ];
    render(
      <WeightEditor
        detail={detail(items)}
        readiness={readiness({
          assets: [
            {
              item_id: "a",
              asset_key: items[0]!.asset_key,
              name: "资产a",
              currency: "CNY",
              is_cash: false,
              enabled: true,
              weight: 0.5,
              adjust_policy: "hfq",
              point_type: "adjusted_close",
              listing_status: "active",
              has_history: false,
              point_count: 0,
              stale: false,
            },
            {
              item_id: "b",
              asset_key: items[1]!.asset_key,
              name: "资产b",
              currency: "CNY",
              is_cash: false,
              enabled: true,
              weight: 0.5,
              adjust_policy: "hfq",
              point_type: "adjusted_close",
              listing_status: "active",
              has_history: true,
              point_count: 100,
              stale: false,
              sync_status: "running",
            },
          ],
        })}
        {...baseHandlers}
      />,
    );
    expect(screen.getByText("缺历史")).toBeInTheDocument();
    expect(screen.getByText("同步中")).toBeInTheDocument();
    expect(screen.getByText("现金（无需历史）")).toBeInTheDocument();
  });

  it("shows the estimated common window from readiness", () => {
    render(
      <WeightEditor
        detail={detail([item("a"), item("b", { weight: 0.5 })])}
        readiness={readiness()}
        {...baseHandlers}
      />,
    );
    expect(screen.getByTestId("common-window")).toHaveTextContent(
      "2018-01-01 ~ 2026-07-01",
    );
  });

  it("shows the fixed backward-adjusted return series without a policy selector", () => {
    render(<WeightEditor detail={detail([item("a")])} {...baseHandlers} />);
    expect(screen.getByTestId("return-series-a")).toHaveTextContent(
      "后复权 · 复权收盘价",
    );
    expect(screen.queryByLabelText("复权口径")).not.toBeInTheDocument();
    expect(screen.queryByText("前复权")).not.toBeInTheDocument();
  });

  it("labels a back-end fee code and states transaction fees are excluded", () => {
    render(
      <WeightEditor
        detail={detail([
          item("000157", {
            asset_key: "CN|cn_mutual_fund||000157",
            symbol: "000157",
            name: "富国全球科技互联网股票(QDII)A(后端)",
            instrument_type: "cn_mutual_fund",
            instrument_type_label: "场外基金",
            adjust_policy: "none",
            point_type: "total_return_index",
            canonical_symbol: "100055",
            fee_mode: "back_end",
          }),
        ])}
        {...baseHandlers}
      />,
    );

    expect(screen.getByText(/后端收费 · 共用 100055 净值/)).toBeInTheDocument();
    expect(screen.getByText("不含申购/赎回收费")).toBeInTheDocument();
  });

  it("does not render draggable asset rows or a drag handle", () => {
    render(
      <WeightEditor
        detail={detail([item("a"), item("b", { weight: 0.5 })])}
        {...baseHandlers}
      />,
    );
    expect(screen.getByTestId("item-row-a")).not.toHaveAttribute("draggable");
    expect(screen.getByTestId("item-row-b")).not.toHaveAttribute("draggable");
    expect(screen.queryByText("⠿")).not.toBeInTheDocument();
  });
});
